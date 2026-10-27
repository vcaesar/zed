/* Copyright 2022 Zinc Labs Inc. and Contributors
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

func TestPartitionOfN(t *testing.T) {
	tests := []struct {
		name       string
		index      string
		partitions int
	}{
		{name: "plain name", index: "logs-2024", partitions: 256},
		{name: "empty name", index: "", partitions: 256},
		{name: "unicode name", index: "日志", partitions: 64},
		{name: "invalid count falls back", index: "logs-2024", partitions: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PartitionOfN(tt.index, tt.partitions)
			// deterministic: same input, same bucket, always in range
			assert.Equal(t, got, PartitionOfN(tt.index, tt.partitions))
			want := tt.partitions
			if want <= 0 {
				want = DefaultPartitions
			}
			assert.GreaterOrEqual(t, got, 0)
			assert.Less(t, got, want)
		})
	}

	// different names should not all land in one bucket
	buckets := map[int]bool{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		buckets[PartitionOfN(name, 256)] = true
	}
	assert.Greater(t, len(buckets), 1)
}

func TestPartitionOfUsesConfig(t *testing.T) {
	assert.Equal(t, PartitionOfN("logs", partitionCount()), PartitionOf("logs"))
}

func TestRebalance(t *testing.T) {
	tests := []struct {
		name       string
		current    map[int]string
		nodes      []string
		partitions int
		assert     func(t *testing.T, changes map[int]string, final map[int]string)
	}{
		{
			name:       "first assignment spreads evenly",
			current:    nil,
			nodes:      []string{"a", "b"},
			partitions: 8,
			assert: func(t *testing.T, changes, final map[int]string) {
				assert.Len(t, changes, 8)
				assert.Equal(t, map[string]int{"a": 4, "b": 4}, countOwners(final))
			},
		},
		{
			name:       "node joins, only the surplus moves",
			current:    ownedBy(8, "a", "a", "a", "a", "b", "b", "b", "b"),
			nodes:      []string{"a", "b", "c"},
			partitions: 8,
			assert: func(t *testing.T, changes, final map[int]string) {
				assert.Len(t, changes, 2) // c takes 2, quota is 3/3/2
				counts := countOwners(final)
				assert.Equal(t, 8, counts["a"]+counts["b"]+counts["c"])
				assert.Equal(t, 2, counts["c"])
			},
		},
		{
			name:       "node leaves, its partitions are handed out",
			current:    ownedBy(8, "a", "a", "a", "a", "b", "b", "b", "b"),
			nodes:      []string{"a"},
			partitions: 8,
			assert: func(t *testing.T, changes, final map[int]string) {
				assert.Len(t, changes, 4)
				assert.Equal(t, map[string]int{"a": 8}, countOwners(final))
			},
		},
		{
			name:       "node replaced keeps the survivors in place",
			current:    ownedBy(8, "a", "a", "a", "a", "b", "b", "b", "b"),
			nodes:      []string{"a", "c"},
			partitions: 8,
			assert: func(t *testing.T, changes, final map[int]string) {
				assert.Len(t, changes, 4)
				for partition := 0; partition < 4; partition++ {
					assert.Equal(t, "a", final[partition], "partition %d moved", partition)
				}
				assert.Equal(t, map[string]int{"a": 4, "c": 4}, countOwners(final))
			},
		},
		{
			name:       "stable when nothing changed",
			current:    ownedBy(8, "a", "a", "a", "a", "b", "b", "b", "b"),
			nodes:      []string{"a", "b"},
			partitions: 8,
			assert: func(t *testing.T, changes, final map[int]string) {
				assert.Empty(t, changes)
			},
		},
		{
			name:       "no nodes, nothing to do",
			current:    ownedBy(4, "a", "a", "a", "a"),
			nodes:      nil,
			partitions: 4,
			assert: func(t *testing.T, changes, final map[int]string) {
				assert.Nil(t, changes)
			},
		},
		{
			name:       "uneven split gives the remainder to the first nodes",
			current:    nil,
			nodes:      []string{"a", "b", "c"},
			partitions: 7,
			assert: func(t *testing.T, changes, final map[int]string) {
				counts := countOwners(final)
				assert.Equal(t, 3, counts["a"])
				assert.Equal(t, 2, counts["b"])
				assert.Equal(t, 2, counts["c"])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := Rebalance(tt.current, tt.nodes, tt.partitions)
			final := make(map[int]string, tt.partitions)
			for partition, owner := range tt.current {
				final[partition] = owner
			}
			for partition, owner := range changes {
				final[partition] = owner
			}
			tt.assert(t, changes, final)
		})
	}
}

func TestAssignerSetRejectsStaleRevision(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	assigner := NewAssigner(store, "/test/", 4)
	first, ok, err := assigner.Set(ctx, 1, "node-a", 0)
	require.NoError(t, err)
	require.True(t, ok)

	// somebody else moved the partition in the meantime
	second, ok, err := assigner.Set(ctx, 1, "node-b", first.Epoch)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Greater(t, second.Epoch, first.Epoch)

	_, ok, err = assigner.Set(ctx, 1, "node-c", first.Epoch)
	require.NoError(t, err)
	assert.False(t, ok)

	assignments, err := assigner.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, "node-b", assignments[1].Owner)
}

func TestAssignerApplySkipsConcurrentChange(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	assigner := NewAssigner(store, "/test/", 4)
	current, err := assigner.List(ctx)
	require.NoError(t, err)
	require.NoError(t, assigner.Apply(ctx, current, map[int]string{0: "node-a", 1: "node-a"}))

	current, err = assigner.List(ctx)
	require.NoError(t, err)
	// partition 1 moves behind the caller's back
	_, ok, err := assigner.Set(ctx, 1, "node-c", current[1].Epoch)
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, assigner.Apply(ctx, current, map[int]string{0: "node-b", 1: "node-b"}))
	assignments, err := assigner.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, "node-b", assignments[0].Owner)
	assert.Equal(t, "node-c", assignments[1].Owner, "a concurrently changed partition must be left alone")
}

func TestMemoryStoreLeaseExpiry(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	store := NewMemoryStore()
	store.SetClock(clock.Now)
	defer func() { require.NoError(t, store.Close()) }()

	lease, err := store.Grant(ctx, 10*time.Second)
	require.NoError(t, err)
	_, err = store.Put(ctx, "/test/k", []byte("v"), lease)
	require.NoError(t, err)

	clock.Advance(5 * time.Second)
	require.NoError(t, store.KeepAlive(ctx, lease))
	clock.Advance(8 * time.Second)
	_, err = store.Get(ctx, "/test/k")
	require.NoError(t, err, "a kept alive lease must survive")

	clock.Advance(20 * time.Second)
	_, err = store.Get(ctx, "/test/k")
	assert.Error(t, err)
}

func ownedBy(partitions int, owners ...string) map[int]string {
	current := make(map[int]string, partitions)
	for partition := 0; partition < partitions && partition < len(owners); partition++ {
		current[partition] = owners[partition]
	}
	return current
}

func countOwners(final map[int]string) map[string]int {
	counts := make(map[string]int)
	for _, owner := range final {
		counts[owner]++
	}
	return counts
}
