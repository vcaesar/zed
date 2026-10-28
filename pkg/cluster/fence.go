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

package cluster

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// ErrNoOwner is returned when nothing owns the partition of an index yet,
// which happens before the first rebalance has been applied.
var ErrNoOwner = errors.New("cluster: index has no owner")

// EpochError says the partition of an index moved: the epoch presented by
// the caller is not the epoch currently recorded in the ownership map.
type EpochError struct {
	Index string
	Owner string
	Have  int64
	Want  int64
}

func (e *EpochError) Error() string {
	return fmt.Sprintf("cluster: partition of index %q moved to %q (epoch %d, presented %d)",
		e.Index, e.Owner, e.Want, e.Have)
}

// Fence answers "who owns this index and with which epoch" from a local
// copy of the ownership map kept current by a watch.
//
// The epoch is the store revision of the assignment key, so it grows on
// every handoff. A request that started while this node was the owner
// carries the epoch it saw; if the partition moved meanwhile the epoch no
// longer matches and the write is rejected with 409.
//
// This closes the transfer window - the interval between the new owner
// taking the partition and the old owner noticing - it does not roll back
// an append that already reached the WAL. A write that was accepted before
// the handoff stays accepted; what is prevented is reporting success for a
// write whose partition is no longer ours.
type Fence struct {
	assigner   *Assigner
	nodeID     string
	partitions int

	mu          sync.RWMutex
	assignments map[int]Assignment
	replicas    map[int][]string
}

// NewFence builds an empty fence, call Refresh or Run to fill it.
func NewFence(assigner *Assigner, nodeID string, partitions int) *Fence {
	if partitions <= 0 {
		partitions = DefaultPartitions
	}
	return &Fence{
		assigner:    assigner,
		nodeID:      nodeID,
		partitions:  partitions,
		assignments: make(map[int]Assignment),
		replicas:    make(map[int][]string),
	}
}

// NodeID is the local node id the fence compares owners against.
func (f *Fence) NodeID() string {
	return f.nodeID
}

// Refresh replaces the local copy with a full read of the ownership map.
func (f *Fence) Refresh(ctx context.Context) error {
	assignments, err := f.assigner.List(ctx)
	if err != nil {
		return err
	}
	replicas, _, err := f.assigner.ListReplicas(ctx)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.assignments = assignments
	f.replicas = replicas
	f.mu.Unlock()
	return nil
}

// Run keeps the local copy current until ctx is done: the watch stream, one
// full read, with a periodic re-read so a dropped watch heals. The watches
// open before the read so no change falls between the two.
func (f *Fence) Run(ctx context.Context, resync time.Duration) {
	events, err := f.assigner.store.Watch(ctx, f.assigner.AssignPrefix())
	if err != nil {
		log.Error().Err(err).Msg("cluster: watch assignments")
		events = nil
	}
	replicaEvents, err := f.assigner.store.Watch(ctx, f.assigner.ReplicasKey())
	if err != nil {
		log.Error().Err(err).Msg("cluster: watch replicas")
		replicaEvents = nil
	}
	if err := f.Refresh(ctx); err != nil {
		log.Error().Err(err).Msg("cluster: load assignments")
	}
	ticker := time.NewTicker(resync)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := f.Refresh(ctx); err != nil && ctx.Err() == nil {
				log.Error().Err(err).Msg("cluster: refresh assignments")
			}
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			f.apply(event)
		case event, ok := <-replicaEvents:
			if !ok {
				replicaEvents = nil
				continue
			}
			f.applyReplicas(event)
		}
	}
}

func (f *Fence) applyReplicas(event Event) {
	if event.KV.Key != f.assigner.ReplicasKey() {
		return
	}
	replicas := make(map[int][]string)
	if event.Type == EventPut {
		var err error
		if replicas, err = decodeReplicas(event.KV.Value); err != nil {
			log.Error().Err(err).Str("key", event.KV.Key).Msg("cluster: bad replica map")
			return
		}
	}
	f.mu.Lock()
	f.replicas = replicas
	f.mu.Unlock()
}

func (f *Fence) apply(event Event) {
	prefix := f.assigner.AssignPrefix()
	if len(event.KV.Key) <= len(prefix) {
		return
	}
	partition, err := strconv.Atoi(event.KV.Key[len(prefix):])
	if err != nil {
		log.Error().Str("key", event.KV.Key).Msg("cluster: bad assignment key")
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	// an event queued before a Refresh must not roll back newer ownership
	if current, ok := f.assignments[partition]; ok && event.KV.Revision <= current.Epoch {
		return
	}
	if event.Type == EventDelete {
		delete(f.assignments, partition)
		return
	}
	var value assignmentValue
	if err := json.Unmarshal(event.KV.Value, &value); err != nil {
		log.Error().Err(err).Str("key", event.KV.Key).Msg("cluster: bad assignment value")
		return
	}
	f.assignments[partition] = Assignment{Partition: partition, Owner: value.Owner, Epoch: event.KV.Revision}
}

// Owner returns the node that owns indexName and the epoch of that
// ownership. ok is false while the partition is unassigned.
func (f *Fence) Owner(indexName string) (string, int64, bool) {
	partition := PartitionOfN(indexName, f.partitions)
	f.mu.RLock()
	defer f.mu.RUnlock()
	assignment, ok := f.assignments[partition]
	if !ok || assignment.Owner == "" {
		return "", 0, false
	}
	return assignment.Owner, assignment.Epoch, true
}

// IsLocalOwner reports whether this node owns indexName. An unassigned
// partition counts as local: before the first rebalance every node serves
// its own requests, exactly like the single node build does.
func (f *Fence) IsLocalOwner(indexName string) bool {
	owner, _, ok := f.Owner(indexName)
	return !ok || owner == f.nodeID
}

// Replicas returns the nodes holding a warm read replica of indexName.
func (f *Fence) Replicas(indexName string) []string {
	partition := PartitionOfN(indexName, f.partitions)
	f.mu.RLock()
	defer f.mu.RUnlock()
	return append([]string(nil), f.replicas[partition]...)
}

// IsLocalReplica reports whether this node is a read replica of indexName.
func (f *Fence) IsLocalReplica(indexName string) bool {
	partition := PartitionOfN(indexName, f.partitions)
	f.mu.RLock()
	defer f.mu.RUnlock()
	return slices.Contains(f.replicas[partition], f.nodeID)
}

// CheckEpoch fails when the ownership of indexName changed since epoch was
// taken, or when this node is no longer the owner.
func (f *Fence) CheckEpoch(indexName string, epoch int64) error {
	owner, current, ok := f.Owner(indexName)
	if !ok {
		if epoch == 0 {
			return nil // still unassigned, same state the request started in
		}
		return ErrNoOwner
	}
	if current != epoch || owner != f.nodeID {
		return &EpochError{Index: indexName, Owner: owner, Have: epoch, Want: current}
	}
	return nil
}

// Assignments returns a copy of the ownership map, replicas included.
func (f *Fence) Assignments() map[int]Assignment {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make(map[int]Assignment, len(f.assignments))
	for partition, assignment := range f.assignments {
		assignment.Replicas = append([]string(nil), f.replicas[partition]...)
		out[partition] = assignment
	}
	return out
}

// PartitionCounts returns how many partitions each node owns.
func (f *Fence) PartitionCounts() map[string]int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	counts := make(map[string]int)
	for _, assignment := range f.assignments {
		counts[assignment.Owner]++
	}
	return counts
}
