/* Copyright 2026 AtomAI
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
 */

package core

import (
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis"

	"github.com/vcaesar/zedsearch/pkg/bluge/directory"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/errors"
	"github.com/vcaesar/zedsearch/pkg/meta"
)

// Data tiers. An index is hot on local disk, warm on object storage while still
// writable by its owner, and cold on object storage read-only, where any node
// may open it at the same time.
const (
	TierHot  = "hot"
	TierWarm = "warm"
	TierCold = "cold"
)

// walFlushTimeout bounds how long a tier move waits for the WAL consumer to
// apply everything that was written before the move started.
const walFlushTimeout = 60 * time.Second

// ErrIndexReadOnly is returned by every write path of a cold index.
var ErrIndexReadOnly = stderrors.New("index is cold (read-only)")

// tierMoveLock serializes tier moves so two callers cannot copy the same index.
var tierMoveLock sync.Mutex

// CheckTier validates a tier name.
func CheckTier(tier string) error {
	switch tier {
	case TierHot, TierWarm, TierCold:
		return nil
	default:
		return fmt.Errorf("tier [%s] is invalid, just accept [hot, warm, cold]", tier)
	}
}

// tierStorageType returns the storage type an index must live on for a tier.
func tierStorageType(tier string) (string, error) {
	if tier == TierHot {
		return StorageTypeDisk, nil
	}
	storageType := config.Global.Tier.ColdStorageType
	if storageType == "" {
		storageType = StorageTypeS3
	}
	if storageType == StorageTypeDisk {
		return "", fmt.Errorf("tier [%s] requires an object storage, ZED_TIER_COLD_STORAGE_TYPE is [disk]", tier)
	}
	if err := CheckStorageType(storageType); err != nil {
		return "", err
	}
	return storageType, nil
}

// GetTier returns the tier of the index; an index without one is hot.
func (index *Index) GetTier() string {
	index.lock.RLock()
	defer index.lock.RUnlock()
	if index.ref.Settings == nil || index.ref.Settings.Tier == "" {
		return TierHot
	}
	return index.ref.Settings.Tier
}

// IsReadOnly reports whether the index rejects writes.
func (index *Index) IsReadOnly() bool {
	return index.readOnly.Load()
}

// IsCold reports whether the index is stored read-only on object storage.
func (index *Index) IsCold() bool {
	return index.GetTier() == TierCold
}

// checkWritable is the guard every document write runs first.
func (index *Index) checkWritable() error {
	if index.readOnly.Load() {
		return ErrIndexReadOnly
	}
	return nil
}

func (index *Index) defaultSearchAnalyzer() *analysis.Analyzer {
	analyzers := index.GetAnalyzers()
	if analyzers == nil {
		return nil
	}
	return analyzers["default"]
}

// secondShardName is the storage name of one second layer shard, identical for
// the local disk directory and the object storage key prefix.
func (s *IndexShard) secondShardName(shardID int64) string {
	return fmt.Sprintf("%s/%s/%06x", s.GetIndexName(), s.GetID(), shardID)
}

// prepareReaders makes the second layer shards searchable and returns how many
// there are. Cold shards are opened per reader, so nothing has to be prepared.
func (s *IndexShard) prepareReaders() (int64, error) {
	if !s.root.IsCold() {
		if _, err := s.GetWriters(); err != nil {
			return 0, err
		}
	}
	return s.GetShardNum(), nil
}

// openReader returns a searcher for one second layer shard. A cold shard is
// opened straight from object storage, without a writer or a WAL, so several
// nodes can read it at once. The caller closes the reader. A cold shard that
// was never written to has nothing to open and yields a nil reader.
func (s *IndexShard) openReader(shardID int64) (*riot.Reader, error) {
	if !s.root.IsCold() {
		w, err := s.GetWriter(shardID)
		if err != nil {
			return nil, err
		}
		return w.Reader()
	}
	name := s.secondShardName(shardID)
	storageType := s.root.GetStorageType()
	r, err := OpenIndexReader(name, storageType, s.root.defaultSearchAnalyzer())
	if err == nil {
		return r, nil
	}
	if hasData, dataErr := directory.ObjectIndexHasData(storageType, name); dataErr == nil && !hasData {
		return nil, nil
	}
	return nil, err
}

// shardDataNames lists the storage name of every second layer shard.
func (index *Index) shardDataNames() []string {
	index.lock.RLock()
	names := make([]string, 0, len(index.ref.Shards))
	for id, shard := range index.ref.Shards {
		for j := int64(0); j < shard.ShardNum; j++ {
			names = append(names, fmt.Sprintf("%s/%s/%06x", index.ref.Name, id, j))
		}
	}
	index.lock.RUnlock()
	sort.Strings(names)
	return names
}

// materialize opens every writable shard once, so an index that was never
// written still has a directory with a snapshot to copy and reopen.
func (index *Index) materialize() error {
	if index.IsCold() {
		return nil
	}
	for _, shard := range index.shards {
		if _, err := shard.GetWriters(); err != nil {
			return err
		}
	}
	return nil
}

// walPending reports whether the shard still has WAL entries the consumer has
// not applied to the writers yet.
func (s *IndexShard) walPending() bool {
	s.lock.RLock()
	w := s.wal
	s.lock.RUnlock()
	if w == nil {
		return false
	}
	last, err := w.LastIndex()
	if err != nil {
		log.Error().Err(err).Str("shard", s.GetShardName()).Msg("tier move: wal.LastIndex()")
		return false
	}
	_, committed, err := s.readRedoLog(RedoActionWrite)
	if err != nil && err.Error() != errors.ErrNotFound.Error() {
		log.Error().Err(err).Str("shard", s.GetShardName()).Msg("tier move: wal.readRedoLog()")
		return false
	}
	return committed < last
}

// flushAndClose waits for the WAL consumer to apply every pending entry, then
// closes the writers and WALs. The consumer is left to do the work so a move
// never races it into writing the same document twice.
func (index *Index) flushAndClose() error {
	deadline := time.Now().Add(walFlushTimeout)
	for _, shard := range index.shards {
		for shard.walPending() {
			if time.Now().After(deadline) {
				return fmt.Errorf("shard [%s]: WAL not applied within %s", shard.GetShardName(), walFlushTimeout)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	return index.Close()
}

// MoveIndexTier moves an index between hot, warm and cold.
//
// Writes are stopped, the WAL is drained and the writers are closed, then every
// segment of every shard is copied to the new location, the new location is
// persisted and the index is reopened (read-only when cold). The old copy is
// dropped only after that succeeded, which makes the move idempotent: running
// it again after a crash re-copies and finishes the pending steps.
func MoveIndexTier(name, tier string) error {
	if err := CheckTier(tier); err != nil {
		return err
	}
	index, exists := GetIndex(name)
	if !exists {
		return fmt.Errorf("index [%s] does not exists", name)
	}
	storageType, err := tierStorageType(tier)
	if err != nil {
		return err
	}

	tierMoveLock.Lock()
	defer tierMoveLock.Unlock()

	from := index.GetStorageType()
	names := index.shardDataNames()
	if index.GetTier() == tier && from == storageType {
		index.applyTier(tier)
		return index.dropOldCopy(tier, from, names)
	}

	log.Info().Str("index", name).Str("from", index.GetTier()).Str("to", tier).Msg("tier move start")
	wasReadOnly := index.readOnly.Load()
	index.readOnly.Store(true)
	restore := func(err error) error {
		index.readOnly.Store(wasReadOnly)
		return err
	}

	if err := index.materialize(); err != nil {
		return restore(fmt.Errorf("tier move [%s]: open shards: %w", name, err))
	}
	if err := index.flushAndClose(); err != nil {
		return restore(fmt.Errorf("tier move [%s]: flush: %w", name, err))
	}
	// draining the WAL may have rolled over to a new second layer shard
	names = index.shardDataNames()
	if err := copyShards(from, storageType, names); err != nil {
		return restore(fmt.Errorf("tier move [%s]: copy: %w", name, err))
	}

	if err := index.setLocation(storageType, tier); err != nil {
		return restore(fmt.Errorf("tier move [%s]: store: %w", name, err))
	}

	index.applyTier(tier)
	// a search during the copy reopens writers on the old location, drop
	// them so the next request opens the new one before the old copy goes
	if err := index.Close(); err != nil {
		return fmt.Errorf("tier move [%s]: close old location: %w", name, err)
	}
	if err := index.reopen(); err != nil {
		return fmt.Errorf("tier move [%s]: reopen: %w", name, err)
	}
	if err := index.dropOldCopy(tier, from, names); err != nil {
		return fmt.Errorf("tier move [%s]: cleanup: %w", name, err)
	}
	log.Info().Str("index", name).Str("tier", tier).Str("storage_type", storageType).Msg("tier move done")
	return nil
}

// applyTier syncs the in-memory write guard with the tier.
func (index *Index) applyTier(tier string) {
	index.readOnly.Store(tier == TierCold)
}

// setLocation persists the new storage type and tier, reverting the in-memory
// state when the metadata store rejects it.
func (index *Index) setLocation(storageType, tier string) error {
	index.lock.Lock()
	oldStorageType := index.ref.StorageType
	oldTier := ""
	if index.ref.Settings == nil {
		index.ref.Settings = new(meta.IndexSettings)
	} else {
		oldTier = index.ref.Settings.Tier
	}
	index.ref.StorageType = storageType
	index.ref.Settings.Tier = tier
	index.lock.Unlock()

	if err := storeIndex(index); err != nil {
		index.lock.Lock()
		index.ref.StorageType = oldStorageType
		index.ref.Settings.Tier = oldTier
		index.lock.Unlock()
		return err
	}
	return nil
}

func copyShards(from, to string, names []string) error {
	if from == to {
		return nil
	}
	for _, name := range names {
		var err error
		if to == StorageTypeDisk {
			err = directory.CopyObjectToDisk(from, config.Global.DataPath, name)
		} else {
			err = directory.CopyDiskToObject(to, config.Global.DataPath, name)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// reopen verifies the index can be served from its new location. Cold shards
// are opened read-only; writable shards open lazily on the next request.
func (index *Index) reopen() error {
	if !index.IsCold() {
		return nil
	}
	for _, shard := range index.shards {
		for j := int64(0); j < shard.GetShardNum(); j++ {
			r, err := shard.openReader(j)
			if err != nil {
				return err
			}
			if r == nil {
				continue
			}
			if err := r.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

// dropOldCopy removes the copy the index no longer reads from.
func (index *Index) dropOldCopy(tier, from string, names []string) error {
	if tier == TierHot {
		if from == StorageTypeDisk {
			return nil
		}
		for _, name := range names {
			if err := directory.RemoveObjectIndexByType(from, name); err != nil {
				return err
			}
		}
		return nil
	}
	for _, name := range names {
		if err := os.RemoveAll(filepath.Join(config.Global.DataPath, filepath.FromSlash(name))); err != nil {
			return err
		}
	}
	return nil
}
