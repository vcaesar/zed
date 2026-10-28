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
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fencePartitions = 8

func newTestFence(t *testing.T, store Store, nodeID string) (*Fence, *Assigner) {
	t.Helper()
	assigner := NewAssigner(store, "/test/", fencePartitions)
	return NewFence(assigner, nodeID, fencePartitions), assigner
}

func TestFenceOwner(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	fence, assigner := newTestFence(t, store, "node-a")
	const index = "logs-2024"
	partition := PartitionOfN(index, fencePartitions)

	// nothing assigned yet: no owner, but writes are allowed locally
	owner, epoch, ok := fence.Owner(index)
	assert.False(t, ok)
	assert.Empty(t, owner)
	assert.Zero(t, epoch)
	assert.True(t, fence.IsLocalOwner(index))
	assert.NoError(t, fence.CheckEpoch(index, 0))

	assignment, ok, err := assigner.Set(ctx, partition, "node-a", 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, fence.Refresh(ctx))

	owner, epoch, ok = fence.Owner(index)
	assert.True(t, ok)
	assert.Equal(t, "node-a", owner)
	assert.Equal(t, assignment.Epoch, epoch)
	assert.True(t, fence.IsLocalOwner(index))
	assert.Equal(t, map[string]int{"node-a": 1}, fence.PartitionCounts())
}

// A watch event queued before a Refresh must not roll back newer ownership.
func TestFenceIgnoresStaleEvent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	fence, assigner := newTestFence(t, store, "node-a")
	const index = "logs-2024"
	partition := PartitionOfN(index, fencePartitions)

	first, ok, err := assigner.Set(ctx, partition, "node-a", 0)
	require.NoError(t, err)
	require.True(t, ok)
	second, ok, err := assigner.Set(ctx, partition, "node-b", first.Epoch)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, fence.Refresh(ctx))

	key := assigner.AssignPrefix() + strconv.Itoa(partition)
	fence.apply(Event{Type: EventPut, KV: KV{Key: key, Value: []byte(`{"owner":"node-a"}`), Revision: first.Epoch}})
	owner, epoch, ok := fence.Owner(index)
	require.True(t, ok)
	assert.Equal(t, "node-b", owner)
	assert.Equal(t, second.Epoch, epoch)

	// a newer delete still applies
	fence.apply(Event{Type: EventDelete, KV: KV{Key: key, Revision: second.Epoch + 1}})
	_, _, ok = fence.Owner(index)
	assert.False(t, ok)
}

func TestFenceCheckEpoch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	fence, assigner := newTestFence(t, store, "node-a")
	const index = "logs-2024"
	partition := PartitionOfN(index, fencePartitions)

	first, ok, err := assigner.Set(ctx, partition, "node-a", 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, fence.Refresh(ctx))

	tests := []struct {
		name      string
		prepare   func(t *testing.T)
		epoch     func() int64
		wantErr   bool
		wantOwner string
	}{
		{
			name:    "current epoch passes",
			prepare: func(t *testing.T) {},
			epoch:   func() int64 { return first.Epoch },
		},
		{
			name:    "stale epoch is rejected",
			prepare: func(t *testing.T) {},
			epoch:   func() int64 { return first.Epoch - 1 },
			wantErr: true, wantOwner: "node-a",
		},
		{
			name: "partition moved during the request",
			prepare: func(t *testing.T) {
				_, ok, err := assigner.Set(ctx, partition, "node-b", first.Epoch)
				require.NoError(t, err)
				require.True(t, ok)
				require.NoError(t, fence.Refresh(ctx))
			},
			epoch:   func() int64 { return first.Epoch },
			wantErr: true, wantOwner: "node-b",
		},
		{
			name: "unassigned after a delete",
			prepare: func(t *testing.T) {
				require.NoError(t, store.Delete(ctx, "/test/assign/"+strconv.Itoa(partition)))
				require.NoError(t, fence.Refresh(ctx))
			},
			epoch:   func() int64 { return first.Epoch },
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.prepare(t)
			err := fence.CheckEpoch(index, tt.epoch())
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			if tt.wantOwner == "" {
				assert.ErrorIs(t, err, ErrNoOwner)
				return
			}
			var epochErr *EpochError
			require.True(t, errors.As(err, &epochErr))
			assert.Equal(t, tt.wantOwner, epochErr.Owner)
			assert.Equal(t, index, epochErr.Index)
		})
	}
}

func TestFenceWatchTracksHandoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	fence, assigner := newTestFence(t, store, "node-a")
	const index = "logs-2024"
	partition := PartitionOfN(index, fencePartitions)

	go fence.Run(ctx, time.Minute)

	first, ok, err := assigner.Set(ctx, partition, "node-a", 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Eventually(t, func() bool {
		owner, epoch, ok := fence.Owner(index)
		return ok && owner == "node-a" && epoch == first.Epoch
	}, 2*time.Second, 5*time.Millisecond)
	assert.True(t, fence.IsLocalOwner(index))

	_, ok, err = assigner.Set(ctx, partition, "node-b", first.Epoch)
	require.NoError(t, err)
	require.True(t, ok)
	require.Eventually(t, func() bool {
		owner, _, ok := fence.Owner(index)
		return ok && owner == "node-b"
	}, 2*time.Second, 5*time.Millisecond)

	assert.False(t, fence.IsLocalOwner(index))
	assert.Error(t, fence.CheckEpoch(index, first.Epoch))
}
