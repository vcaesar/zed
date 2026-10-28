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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanReplicas(t *testing.T) {
	owners := ownedBy(6, "a", "a", "a", "b", "b", "c")
	tests := []struct {
		name   string
		nodes  []string
		n      int
		assert func(t *testing.T, got map[int][]string)
	}{
		{
			name:  "zero replicas plans nothing",
			nodes: []string{"a", "b", "c"},
			n:     0,
			assert: func(t *testing.T, got map[int][]string) {
				assert.Empty(t, got)
			},
		},
		{
			name:  "one replica, never the owner, spread over the others",
			nodes: []string{"c", "a", "b"},
			n:     1,
			assert: func(t *testing.T, got map[int][]string) {
				require.Len(t, got, 6)
				used := map[string]bool{}
				for partition, replicas := range got {
					require.Len(t, replicas, 1)
					assert.NotEqual(t, owners[partition], replicas[0])
					if owners[partition] == "a" {
						used[replicas[0]] = true
					}
				}
				assert.Equal(t, map[string]bool{"b": true, "c": true}, used,
					"the partitions of one owner must not all land on one replica")
			},
		},
		{
			name:  "more replicas than nodes is capped",
			nodes: []string{"a", "b", "c"},
			n:     5,
			assert: func(t *testing.T, got map[int][]string) {
				for partition, replicas := range got {
					assert.Len(t, replicas, 2)
					assert.NotContains(t, replicas, owners[partition])
				}
			},
		},
		{
			name:  "single node has nobody to replicate to",
			nodes: []string{"a"},
			n:     1,
			assert: func(t *testing.T, got map[int][]string) {
				for partition, owner := range owners {
					if owner == "a" {
						assert.NotContains(t, got, partition)
					}
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlanReplicas(owners, tt.nodes, tt.n)
			assert.Equal(t, got, PlanReplicas(owners, tt.nodes, tt.n), "planning must be deterministic")
			tt.assert(t, got)
		})
	}
}

func TestRebalanceWarmPromotesReplica(t *testing.T) {
	tests := []struct {
		name     string
		current  map[int]string
		replicas map[int][]string
		nodes    []string
		want     map[int]string
	}{
		{
			name:     "dead owner hands over to its live replica",
			current:  ownedBy(4, "a", "a", "b", "b"),
			replicas: map[int][]string{2: {"a"}, 3: {"a"}},
			nodes:    []string{"a", "c"},
			// a is at quota 2, so the warm replica cannot take both
			want: map[int]string{2: "c", 3: "c"},
		},
		{
			name:     "replica with room wins over node order",
			current:  ownedBy(4, "a", "a", "b", "b"),
			replicas: map[int][]string{2: {"c"}, 3: {"d"}},
			nodes:    []string{"a", "c", "d"},
			want:     map[int]string{2: "c", 3: "d"},
		},
		{
			name:     "dead replica is skipped",
			current:  ownedBy(4, "a", "a", "b", "b"),
			replicas: map[int][]string{2: {"x", "d"}},
			nodes:    []string{"a", "c", "d"},
			want:     map[int]string{2: "d", 3: "c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, RebalanceWarm(tt.current, tt.replicas, tt.nodes, 4))
		})
	}
}

func TestAssignerReplicas(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()
	assigner := NewAssigner(store, "/test/", 4)

	replicas, rev, err := assigner.ListReplicas(ctx)
	require.NoError(t, err)
	assert.Empty(t, replicas)
	assert.Zero(t, rev)

	want := map[int][]string{0: {"b"}, 3: {"a", "c"}}
	ok, err := assigner.SetReplicas(ctx, want, rev)
	require.NoError(t, err)
	require.True(t, ok)

	got, newRev, err := assigner.ListReplicas(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Positive(t, newRev)

	ok, err = assigner.SetReplicas(ctx, map[int][]string{}, rev)
	require.NoError(t, err)
	assert.False(t, ok, "a stale revision must be rejected")

	// a replica change must leave the ownership epoch alone
	first, ok, err := assigner.Set(ctx, 0, "a", 0)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = assigner.SetReplicas(ctx, map[int][]string{0: {"c"}}, newRev)
	require.NoError(t, err)
	require.True(t, ok)
	assignments, err := assigner.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, first.Epoch, assignments[0].Epoch)

	_, err = store.Put(ctx, assigner.ReplicasKey(), []byte("not json"), NoLease)
	require.NoError(t, err)
	_, _, err = assigner.ListReplicas(ctx)
	assert.Error(t, err)
}

func TestFenceTracksReplicas(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	fence, assigner := newTestFence(t, store, "node-a")
	const index = "logs-2024"
	partition := PartitionOfN(index, fencePartitions)
	go fence.Run(ctx, time.Minute)

	assert.False(t, fence.IsLocalReplica(index))
	_, ok, err := assigner.Set(ctx, partition, "node-b", 0)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = assigner.SetReplicas(ctx, map[int][]string{partition: {"node-a", "node-c"}}, 0)
	require.NoError(t, err)
	require.True(t, ok)

	require.Eventually(t, func() bool { return fence.IsLocalReplica(index) }, 2*time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"node-a", "node-c"}, fence.Replicas(index))
	assert.False(t, fence.IsLocalOwner(index), "a replica is not an owner")
	require.Eventually(t, func() bool {
		return len(fence.Assignments()[partition].Replicas) == 2
	}, 2*time.Second, 5*time.Millisecond)

	// returned slices are copies
	fence.Replicas(index)[0] = "mutated"
	assert.Equal(t, "node-a", fence.Replicas(index)[0])

	require.NoError(t, store.Delete(ctx, assigner.ReplicasKey()))
	require.Eventually(t, func() bool { return !fence.IsLocalReplica(index) }, 2*time.Second, 5*time.Millisecond)
}

// gapStore runs beforeWatch as a watch on key opens, a write landing as Run
// starts.
type gapStore struct {
	*MemoryStore
	key         string
	beforeWatch func()
}

func (s *gapStore) Watch(ctx context.Context, prefix string) (<-chan Event, error) {
	if prefix == s.key && s.beforeWatch != nil {
		s.beforeWatch()
	}
	return s.MemoryStore.Watch(ctx, prefix)
}

// A change between the first read and the watch must not be lost until the
// next resync.
func TestFenceRunSeesChangeBeforeWatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &gapStore{MemoryStore: NewMemoryStore()}
	defer func() { require.NoError(t, store.Close()) }()

	fence, assigner := newTestFence(t, store, "node-a")
	const index = "logs-2024"
	partition := PartitionOfN(index, fencePartitions)
	store.key = assigner.ReplicasKey()
	store.beforeWatch = func() {
		ok, err := assigner.SetReplicas(ctx, map[int][]string{partition: {"node-a"}}, 0)
		assert.NoError(t, err)
		assert.True(t, ok)
	}
	go fence.Run(ctx, time.Hour)

	require.Eventually(t, func() bool { return fence.IsLocalReplica(index) }, 2*time.Second, 5*time.Millisecond)
}

func TestCoordinatorWritesReplicas(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	membership := testMembership(store, "node-a", time.Minute)
	require.NoError(t, membership.Join(ctx))
	nodeB := testMembership(store, "node-b", time.Minute)
	require.NoError(t, nodeB.Join(ctx))

	assigner := NewAssigner(store, "/test/", 8)
	election := NewElection(store, "/test/", "node-a", time.Minute, time.Second)
	coordinator := NewCoordinator(election, membership, assigner, 8, 1, time.Second)
	won, err := election.Campaign(ctx)
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, coordinator.RebalanceOnce(ctx))

	assignments, err := assigner.List(ctx)
	require.NoError(t, err)
	replicas, rev, err := assigner.ListReplicas(ctx)
	require.NoError(t, err)
	require.Len(t, replicas, 8)
	for partition, assignment := range assignments {
		require.Len(t, replicas[partition], 1)
		assert.NotEqual(t, assignment.Owner, replicas[partition][0])
	}

	// nothing changed, the replica map is not rewritten
	require.NoError(t, coordinator.RebalanceOnce(ctx))
	_, again, err := assigner.ListReplicas(ctx)
	require.NoError(t, err)
	assert.Equal(t, rev, again)

	// node-b dies: its partitions go to node-a, which was their warm replica
	require.NoError(t, nodeB.Leave(ctx))
	require.NoError(t, coordinator.RebalanceOnce(ctx))
	assignments, err = assigner.List(ctx)
	require.NoError(t, err)
	for _, assignment := range assignments {
		assert.Equal(t, "node-a", assignment.Owner)
	}
	replicas, _, err = assigner.ListReplicas(ctx)
	require.NoError(t, err)
	assert.Empty(t, replicas, "a lone node has no replicas")
}

func TestMembershipAddrCache(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	local := testMembership(store, "node-a", time.Minute)
	require.NoError(t, local.Join(ctx))
	_, ok := local.Addr("node-a")
	assert.False(t, ok, "the cache is empty until Refresh or Run")

	go local.Run(ctx, time.Minute)
	require.Eventually(t, func() bool { _, ok := local.Addr("node-a"); return ok }, 2*time.Second, 5*time.Millisecond)

	peer := NewMembership(store, "/test/", Node{ID: "node-b", Addr: "http://node-b:4080"}, time.Minute, time.Second)
	require.NoError(t, peer.Join(ctx))
	require.Eventually(t, func() bool {
		addr, ok := local.Addr("node-b")
		return ok && addr == "http://node-b:4080"
	}, 2*time.Second, 5*time.Millisecond)

	require.NoError(t, peer.Leave(ctx))
	require.Eventually(t, func() bool { _, ok := local.Addr("node-b"); return !ok }, 2*time.Second, 5*time.Millisecond)
}

func TestClusterReplicasDisabled(t *testing.T) {
	require.False(t, Enabled())
	assert.Nil(t, Replicas("anything"))
	assert.False(t, IsLocalReplica("anything"))
}
