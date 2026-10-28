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

func TestUpdateInSync(t *testing.T) {
	ctx := context.Background()
	assigner := NewAssigner(NewMemoryStore(), "/test/insync/", 8)

	insync, rev, err := assigner.ListInSync(ctx)
	require.NoError(t, err)
	assert.Empty(t, insync)
	assert.Zero(t, rev)

	require.NoError(t, assigner.UpdateInSync(ctx, "logs", []string{"node-c", "node-b"}))
	require.NoError(t, assigner.UpdateInSync(ctx, "empty", []string{}))
	insync, rev, err = assigner.ListInSync(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"logs": {"node-b", "node-c"}, "empty": {}}, insync)

	// unchanged sets do not write
	require.NoError(t, assigner.UpdateInSync(ctx, "logs", []string{"node-b", "node-c"}))
	_, again, err := assigner.ListInSync(ctx)
	require.NoError(t, err)
	assert.Equal(t, rev, again)

	require.NoError(t, assigner.UpdateInSync(ctx, "logs", nil))
	require.NoError(t, assigner.UpdateInSync(ctx, "missing", nil))
	insync, _, err = assigner.ListInSync(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"empty": {}}, insync)
}

func TestEligibleOwners(t *testing.T) {
	const partitions = 1 // every index shares partition 0
	tests := []struct {
		name   string
		insync map[string][]string
		want   map[int][]string
	}{
		{name: "nothing replicated", insync: map[string][]string{}, want: map[int][]string{}},
		{name: "one index", insync: map[string][]string{"a": {"n2", "n1"}}, want: map[int][]string{0: {"n1", "n2"}}},
		{name: "intersection", insync: map[string][]string{"a": {"n1", "n2"}, "b": {"n2", "n3"}}, want: map[int][]string{0: {"n2"}}},
		{name: "empty entry blocks", insync: map[string][]string{"a": {"n1"}, "b": {}}, want: map[int][]string{0: {}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, EligibleOwners(tt.insync, partitions))
		})
	}
}

func TestConstrainChanges(t *testing.T) {
	owners := map[int]string{0: "dead", 1: "n1", 2: "dead", 3: "dead"}
	nodes := []string{"n1", "n2", "n3"}
	eligible := map[int][]string{0: {"n3"}, 1: {"n3"}, 2: {}}
	changes := map[int]string{0: "n2", 1: "n2", 2: "n2", 3: "n2"}

	got := ConstrainChanges(changes, owners, nodes, eligible)
	assert.Equal(t, map[int]string{
		0: "n3", // dead owner: moved to the in-sync replica instead
		// 1: live owner keeps a partition its target is not in sync for
		// 2: no in-sync replica alive, the partition stays unavailable
		3: "n2", // not replicated, unconstrained
	}, got)

	assert.Equal(t, changes, ConstrainChanges(changes, owners, nodes, nil))
}

func TestRebalanceKeepsInSyncOwner(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	prefix := "/test/insync-rebalance/"
	assigner := NewAssigner(store, prefix, 1)

	_, ok, err := assigner.Set(ctx, 0, "gone", 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, assigner.UpdateInSync(ctx, "logs", []string{}))

	membership := NewMembership(store, prefix, Node{ID: "n1"}, time.Minute, time.Second)
	require.NoError(t, membership.Join(ctx))
	election := NewElection(store, prefix, "n1", time.Minute, time.Second)
	won, err := election.Campaign(ctx)
	require.NoError(t, err)
	require.True(t, won)
	coordinator := NewCoordinator(election, membership, assigner, 1, 0, time.Second)
	require.NoError(t, coordinator.RebalanceOnce(ctx))

	current, err := assigner.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, "gone", current[0].Owner, "a stale node must not be promoted")

	require.NoError(t, assigner.UpdateInSync(ctx, "logs", []string{"n1"}))
	require.NoError(t, coordinator.RebalanceOnce(ctx))
	current, err = assigner.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, "n1", current[0].Owner)
}
