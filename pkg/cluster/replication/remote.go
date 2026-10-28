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
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/bluge/directory"
	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// The remote translog of an object-storage index lives next to its
// segments, under `<index>/_translog/<term>-<last seq>`, one object per
// commit. Terms start with the zero padded epoch and start time, so the
// keys sort in history order.

func translogPrefix(index string) string {
	return index + "/_translog/"
}

func translogKey(p *primary, last uint64) string {
	return fmt.Sprintf("%s%s-%020d", translogPrefix(p.index), p.term, last)
}

// parseTranslogKey splits a key into term and last operation.
func parseTranslogKey(prefix, key string) (string, uint64, bool) {
	name := strings.TrimPrefix(key, prefix)
	cut := strings.LastIndexByte(name, '-')
	if cut <= 0 {
		return "", 0, false
	}
	seq, err := strconv.ParseUint(name[cut+1:], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return name[:cut], seq, true
}

// upload puts every operation recorded since the last upload in the
// bucket; a write is acknowledged only after that.
func (m *manager) upload(p *primary) error {
	p.mu.Lock()
	ops, _ := p.opsFrom(p.uploaded+1, p.seq)
	p.mu.Unlock()
	if len(ops) == 0 {
		return nil
	}
	data, err := json.Marshal(ops)
	if err != nil {
		return err
	}
	last := ops[len(ops)-1].Seq
	if err := directory.PutObject(p.storageType, translogKey(p, last), data); err != nil {
		return fmt.Errorf("replication: upload translog of [%s]: %w", p.index, err)
	}
	p.mu.Lock()
	p.uploaded = max(p.uploaded, last)
	p.trim(nil)
	p.mu.Unlock()
	return nil
}

// replayRemote applies the whole remote translog when this node takes an
// object-storage index over. Entries are idempotent, so replaying what the
// segments already hold is harmless; the next flush trims the translog.
func (m *manager) replayRemote(p *primary) error {
	index, ok := core.GetIndex(p.index)
	if !ok {
		return fmt.Errorf("replication: index [%s] does not exist", p.index)
	}
	keys, err := directory.ListObjects(p.storageType, translogPrefix(p.index))
	if err != nil {
		return err
	}
	// keys sort in history order, not every backend lists them sorted
	sort.Strings(keys)
	for _, key := range keys {
		data, err := directory.GetObject(p.storageType, key)
		if err != nil {
			return err
		}
		var ops []Op
		if err := json.Unmarshal(data, &ops); err != nil {
			return fmt.Errorf("replication: translog %s: %w", key, err)
		}
		if err := applyOps(index, ops); err != nil {
			return err
		}
	}
	if len(keys) > 0 {
		log.Info().Str("index", p.index).Int("objects", len(keys)).Msg("replication: remote translog replayed")
	}
	return nil
}

// flushRemote persists the segments of an object-storage index and drops
// the translog they cover: everything of older terms, and this term up to
// the last uploaded operation.
func (m *manager) flushRemote(ctx context.Context, p *primary) error {
	if err := m.commit(ctx, p, false); err != nil {
		return err
	}
	keys, err := directory.ListObjects(p.storageType, translogPrefix(p.index))
	if err != nil || len(keys) == 0 {
		return err
	}
	index, ok := core.GetIndex(p.index)
	if !ok {
		return nil
	}
	p.mu.Lock()
	covered := p.uploaded
	p.mu.Unlock()
	if err := index.Flush(); err != nil {
		return err
	}
	prefix := translogPrefix(p.index)
	for _, key := range keys {
		term, last, ok := parseTranslogKey(prefix, key)
		if !ok || term > p.term || term == p.term && last > covered {
			continue
		}
		if err := directory.DeleteObject(p.storageType, key); err != nil {
			return err
		}
	}
	return nil
}
