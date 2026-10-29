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
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// primary is the owner side of one index: the operation log of the
// current term and what every replica acknowledged.
//
// The term is unique per owner incarnation (partition epoch plus start
// time), so a replica can tell a new owner, or a restarted one, from the
// owner it followed and recover instead of mixing two histories.
type primary struct {
	index  string
	term   string
	remote bool
	// storageType is the bucket of a remote index
	storageType string

	prepared sync.Once
	prepErr  error

	mu       sync.Mutex
	seq      uint64 // last recorded operation
	ops      []Op   // retained operations, ascending and contiguous
	acked    map[string]uint64
	insync   []string
	uploaded uint64   // remote: last operation in the bucket
	sums     sumCache // of the owned segments, for incremental snapshots

	// commitMu serializes shipping, a commit that waited finds its
	// operations already acknowledged by the one before it
	commitMu sync.Mutex
}

// lastTermTime keeps terms of one process strictly increasing: clocks with
// coarse ticks (Windows) would give two incarnations the same term.
var lastTermTime atomic.Int64

func newTerm(epoch int64) string {
	for {
		last, now := lastTermTime.Load(), time.Now().UnixNano()
		if now <= last {
			now = last + 1
		}
		if lastTermTime.CompareAndSwap(last, now) {
			return fmt.Sprintf("%020d-%020d", epoch, now)
		}
	}
}

func (p *primary) dirty() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.remote {
		return p.uploaded < p.seq
	}
	for _, node := range p.insync {
		if p.acked[node] < p.seq {
			return true
		}
	}
	return false
}

// opsFrom returns the retained operations from..to, false when some were
// already dropped.
func (p *primary) opsFrom(from, to uint64) ([]Op, bool) {
	if from > to {
		return nil, true
	}
	if len(p.ops) == 0 || p.ops[0].Seq > from {
		return nil, false
	}
	start := int(from - p.ops[0].Seq)
	end := int(to-p.ops[0].Seq) + 1
	return append([]Op(nil), p.ops[start:end]...), true
}

// trim drops operations every replica (or the bucket) already has, and
// anything beyond retainOps: a replica that far behind recovers from files.
func (p *primary) trim(replicas []string) {
	keep := p.seq
	if p.remote {
		keep = p.uploaded
	}
	for _, node := range replicas {
		keep = min(keep, p.acked[node])
	}
	drop := 0
	for drop < len(p.ops) && p.ops[drop].Seq <= keep {
		drop++
	}
	drop = max(drop, len(p.ops)-retainOps)
	if drop > 0 {
		p.ops = append(p.ops[:0], p.ops[drop:]...)
	}
}

// record is core.WALHook: it appends a local write to the operation log
// of the index when this node owns it.
func (m *manager) record(index, shard string, entry []byte) {
	if !m.topo.IsOwner(index) {
		return
	}
	p := m.primaryFor(index)
	if p == nil {
		return
	}
	p.mu.Lock()
	p.seq++
	p.ops = append(p.ops, Op{Seq: p.seq, Shard: shard, Entry: append(json.RawMessage(nil), entry...)})
	p.mu.Unlock()
}

// primaryFor returns the state of an owned writable index, created on
// first use. Nil for cold indexes, which take no writes.
func (m *manager) primaryFor(name string) *primary {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.primaries[name]; ok {
		return p
	}
	index, ok := core.GetIndex(name)
	if !ok || index.IsCold() {
		return nil
	}
	p := &primary{
		index: name,
		term:  newTerm(m.topo.Epoch(name)),
		acked: make(map[string]uint64),
	}
	if !index.IsDisk() {
		p.remote = true
		p.storageType = index.GetStorageType()
	}
	m.primaries[name] = p
	return p
}

// prepare returns the primary of an owned index, promoting this node the
// first time: operations kept as a segment replica, or the remote
// translog, are replayed and the in-sync entry is set up.
func (m *manager) prepare(ctx context.Context, name string) (*primary, error) {
	if !m.topo.IsOwner(name) {
		return nil, fmt.Errorf("replication: index [%s] is not owned by this node", name)
	}
	if _, err := core.LoadIndex(name); err != nil {
		return nil, err
	}
	p := m.primaryFor(name)
	if p == nil {
		return nil, fmt.Errorf("replication: index [%s] is read-only", name)
	}
	p.prepared.Do(func() { p.prepErr = m.promote(ctx, p) })
	if p.prepErr != nil {
		m.mu.Lock()
		if m.primaries[name] == p {
			delete(m.primaries, name)
		}
		m.mu.Unlock()
		return nil, p.prepErr
	}
	return p, nil
}

func (m *manager) promote(ctx context.Context, p *primary) error {
	m.mu.Lock()
	r := m.replicas[p.index]
	delete(m.replicas, p.index)
	m.mu.Unlock()
	if r != nil {
		if err := r.replayPending(p.index); err != nil {
			return err
		}
	}
	if p.remote {
		return m.replayRemote(p)
	}
	if err := removeState(p.index); err != nil {
		return err
	}

	insync, err := m.topo.InSync(ctx)
	if err != nil {
		return err
	}
	local := m.topo.LocalID()
	nodes := slices.DeleteFunc(append([]string{}, insync[p.index]...), func(node string) bool { return node == local })
	if err := m.topo.UpdateInSync(ctx, p.index, nodes); err != nil {
		return err
	}
	p.mu.Lock()
	p.insync = nodes
	p.mu.Unlock()
	return nil
}

// commit ships everything recorded so far: to the bucket for a remote
// index, to every replica otherwise. A replica that fails is removed from
// the in-sync set before commit returns, OpenSearch's failShardIfNeeded;
// commit fails only when that removal cannot be recorded.
func (m *manager) commit(ctx context.Context, p *primary, probe bool) error {
	p.commitMu.Lock()
	defer p.commitMu.Unlock()
	if p.remote {
		return m.upload(p)
	}

	local := m.topo.LocalID()
	replicas := slices.DeleteFunc(m.topo.Replicas(p.index), func(node string) bool { return node == local })
	p.mu.Lock()
	target := p.seq
	insync := append([]string(nil), p.insync...)
	p.mu.Unlock()

	mappings, err := m.mappings(p.index)
	if err != nil {
		return err
	}
	synced := make([]bool, len(replicas))
	group, groupCtx := errgroup.WithContext(ctx)
	for i, node := range replicas {
		group.Go(func() error {
			synced[i] = m.ship(groupCtx, p, node, target, mappings, probe)
			return nil
		})
	}
	_ = group.Wait() // ship reports through synced, it never fails the group

	next := make([]string, 0, len(replicas))
	for i, node := range replicas {
		if synced[i] {
			next = append(next, node)
		}
	}
	sort.Strings(next)

	p.mu.Lock()
	p.trim(replicas)
	p.mu.Unlock()
	if slices.Equal(next, insync) {
		return nil
	}
	if err := m.topo.UpdateInSync(ctx, p.index, next); err != nil {
		if removed := slices.ContainsFunc(insync, func(node string) bool { return !slices.Contains(next, node) }); removed {
			return fmt.Errorf("replication: cannot mark stale replicas of [%s]: %w", p.index, err)
		}
		log.Warn().Err(err).Str("index", p.index).Msg("replication: record caught up replicas")
		return nil
	}
	p.mu.Lock()
	p.insync = next
	p.mu.Unlock()
	return nil
}

// ship brings one replica to target and reports whether it got there. A
// replica that lost operations is caught up from the retained log when
// possible and asked to recover from files otherwise. Without probe a
// replica that already acknowledged target is not called.
func (m *manager) ship(ctx context.Context, p *primary, node string, target uint64, mappings json.RawMessage, probe bool) bool {
	p.mu.Lock()
	from := p.acked[node] + 1
	known := p.acked[node] > 0
	p.mu.Unlock()
	if !probe && known && from > target {
		return true
	}

	for range 3 {
		p.mu.Lock()
		ops, ok := p.opsFrom(from, target)
		p.mu.Unlock()
		batch := &Batch{Index: p.index, Term: p.term, From: from, Ops: ops}
		if !ok {
			// the operations are gone: ask where the replica is
			batch = &Batch{Index: p.index, Term: p.term, From: target + 1}
		} else if len(ops) > 0 {
			batch.Mappings = mappings
		}
		ack, err := m.transport.Replicate(ctx, node, batch)
		if err == nil {
			p.mu.Lock()
			p.acked[node] = max(p.acked[node], ack.Seq)
			p.mu.Unlock()
			if ack.Seq >= target {
				return true
			}
			from = ack.Seq + 1
			continue
		}
		if !errors.Is(err, ErrOutOfSync) {
			log.Warn().Err(err).Str("index", p.index).Str("replica", node).Msg("replication: replica failed")
			return false
		}
		if ack.Recovering || ack.Term != p.term {
			break
		}
		p.mu.Lock()
		p.acked[node] = ack.Seq
		p.mu.Unlock()
		if !p.retained(ack.Seq+1, target) {
			break
		}
		from = ack.Seq + 1
	}

	if err := m.transport.Recover(ctx, node, p.index); err != nil {
		log.Warn().Err(err).Str("index", p.index).Str("replica", node).Msg("replication: ask replica to recover")
	}
	return false
}

// retained reports whether from..to is still in the operation log.
func (p *primary) retained(from, to uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.opsFrom(from, to)
	return ok
}

func (m *manager) mappings(name string) (json.RawMessage, error) {
	index, ok := core.GetIndex(name)
	if !ok {
		return nil, fmt.Errorf("replication: index [%s] does not exist", name)
	}
	return json.Marshal(index.GetMappings())
}

// checkpoint returns the term and last operation of an owned index.
func (p *primary) checkpoint() (string, uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.term, p.seq
}
