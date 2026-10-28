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

package replication

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// replica is the follower side of one index.
type replica struct {
	mu         sync.Mutex
	term       string // owner history the local copy belongs to
	seq        uint64 // last operation applied (document) or kept (segment)
	filesSeq   uint64 // the installed files contain at least this operation
	ready      bool   // the local copy was installed from the owner
	recovering bool
	pending    []Op // segment: operations newer than the installed files
}

func (r *replica) isReady() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ready
}

func (r *replica) isRecovering() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recovering
}

func (r *replica) ack() Ack {
	return Ack{Term: r.term, Seq: r.seq, Recovering: r.recovering}
}

func (m *manager) replicaFor(name string) *replica {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.replicas[name]
	if !ok {
		r = new(replica)
		m.replicas[name] = r
	}
	return r
}

// Apply handles a batch shipped by the owner. ErrOutOfSync comes with the
// Ack telling the owner what is missing.
func Apply(batch *Batch) (Ack, error) {
	m := get()
	if m == nil {
		return Ack{}, errors.New("replication: not enabled")
	}
	return m.apply(batch)
}

func (m *manager) apply(batch *Batch) (Ack, error) {
	if m.topo.IsOwner(batch.Index) {
		return Ack{}, fmt.Errorf("replication: this node owns [%s]", batch.Index)
	}
	r := m.replicaFor(batch.Index)
	r.mu.Lock()
	defer r.mu.Unlock()
	// a segment replica refreshing its files keeps taking operations, a
	// document replica is not ready while its copy is being replaced
	if !r.ready || r.term != batch.Term || batch.From > r.seq+1 {
		return r.ack(), ErrOutOfSync
	}

	ops := make([]Op, 0, len(batch.Ops))
	for _, op := range batch.Ops {
		if op.Seq > r.seq {
			ops = append(ops, op)
		}
	}
	if len(ops) == 0 {
		return r.ack(), nil
	}
	index, ok := core.GetIndex(batch.Index)
	if !ok {
		return Ack{}, fmt.Errorf("replication: index [%s] is not installed", batch.Index)
	}
	if len(batch.Mappings) > 0 {
		mappings := meta.NewMappings()
		if err := json.Unmarshal(batch.Mappings, mappings); err != nil {
			return Ack{}, err
		}
		if err := index.SetMappings(mappings); err != nil {
			return Ack{}, err
		}
	}
	if m.mode == ModeSegment {
		r.pending = append(r.pending, ops...)
	} else if err := applyOps(index, ops); err != nil {
		return Ack{}, err
	}
	r.seq = ops[len(ops)-1].Seq
	return r.ack(), nil
}

// applyOps appends operations to the WAL of their shards, in order.
func applyOps(index *core.Index, ops []Op) error {
	for start := 0; start < len(ops); {
		end := start
		entries := make([][]byte, 0)
		for end < len(ops) && ops[end].Shard == ops[start].Shard {
			entries = append(entries, ops[end].Entry)
			end++
		}
		if err := core.ApplyReplicaEntries(index, ops[start].Shard, entries); err != nil {
			return err
		}
		start = end
	}
	return nil
}

// replayPending indexes the operations a segment replica kept, it runs
// once when the replica is promoted.
func (r *replica) replayPending(name string) error {
	r.mu.Lock()
	pending := r.pending
	r.pending = nil
	r.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	index, ok := core.GetIndex(name)
	if !ok {
		return fmt.Errorf("replication: index [%s] is not installed", name)
	}
	log.Info().Str("index", name).Int("ops", len(pending)).Msg("replication: replaying kept operations")
	return applyOps(index, pending)
}

// RequestRecovery starts a file copy of index from its owner in the
// background, unless one is running.
func RequestRecovery(index string) error {
	m := get()
	if m == nil {
		return errors.New("replication: not enabled")
	}
	if m.topo.IsOwner(index) {
		return fmt.Errorf("replication: this node owns [%s]", index)
	}
	m.startRecovery(index)
	return nil
}

// startRecovery copies the owner's files in the background: OpenSearch
// peer recovery for document replicas, a checkpoint for segment replicas.
func (m *manager) startRecovery(name string) {
	r := m.replicaFor(name)
	r.mu.Lock()
	if r.recovering {
		r.mu.Unlock()
		return
	}
	r.recovering = true
	have := Checkpoint{}
	if r.ready && m.mode == ModeSegment {
		have = Checkpoint{Term: r.term, Seq: r.filesSeq}
	} else {
		// the copy is replaced: operations applied meanwhile would be lost
		r.ready = false
	}
	r.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		err := m.recover(name, r, have)
		r.mu.Lock()
		r.recovering = false
		r.mu.Unlock()
		if err != nil && !errors.Is(err, ErrNotModified) && m.ctx.Err() == nil {
			log.Warn().Err(err).Str("index", name).Msg("replication: recovery failed")
		}
	}()
}

func (m *manager) recover(name string, r *replica, have Checkpoint) error {
	owner, ok := m.topo.Owner(name)
	if !ok || owner == m.topo.LocalID() {
		return fmt.Errorf("replication: index [%s] has no remote owner", name)
	}
	staging := filepath.Join(config.Global.DataPath, "_replication")
	if err := os.MkdirAll(staging, 0o750); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(staging, "recovery-")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Warn().Err(err).Str("dir", dir).Msg("replication: remove staging")
		}
	}()

	files := filepath.Join(dir, "files")
	info, err := m.transport.Snapshot(m.ctx, owner, name, have, files)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(files, 0o750); err != nil { // an index without data has no files
		return err
	}
	// hold the state while swapping, so no batch lands on the old copy
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := core.InstallSnapshot(name, info.Index, files); err != nil {
		return err
	}
	if r.term != info.Term {
		r.pending = nil
	}
	kept := r.pending[:0]
	for _, op := range r.pending {
		if op.Seq > info.Seq {
			kept = append(kept, op)
		}
	}
	r.pending = kept
	r.term = info.Term
	r.seq = max(info.Seq, lastSeq(kept))
	r.filesSeq = info.Seq
	r.ready = true
	log.Info().Str("index", name).Str("owner", owner).Uint64("seq", info.Seq).Msg("replication: copy installed")
	return nil
}

func lastSeq(ops []Op) uint64 {
	if len(ops) == 0 {
		return 0
	}
	return ops[len(ops)-1].Seq
}

// ServeSnapshot writes a file copy of an owned disk index to w. It answers
// ErrNotModified when the replica already is at have.
func ServeSnapshot(ctx context.Context, name string, have Checkpoint, w func() io.Writer) error {
	m := get()
	if m == nil {
		return errors.New("replication: not enabled")
	}
	p, err := m.prepare(ctx, name)
	if err != nil {
		return err
	}
	if p.remote {
		return fmt.Errorf("replication: index [%s] is on object storage", name)
	}
	index, ok := core.GetIndex(name)
	if !ok {
		return fmt.Errorf("replication: index [%s] does not exist", name)
	}
	term, seq := p.checkpoint()
	if have.Term == term && have.Seq == seq {
		return ErrNotModified
	}

	staging := filepath.Join(config.Global.DataPath, "_replication")
	if err := os.MkdirAll(staging, 0o750); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(staging, "snapshot-")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Warn().Err(err).Str("dir", dir).Msg("replication: remove snapshot")
		}
	}()
	// seq is read before the copy, so the copy contains at least seq
	indexMeta, err := index.SnapshotTo(dir)
	if err != nil {
		return err
	}
	return WriteSnapshot(w(), &SnapshotInfo{Checkpoint: Checkpoint{Term: term, Seq: seq}, Index: indexMeta}, dir)
}
