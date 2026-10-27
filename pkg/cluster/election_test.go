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

func TestElectionSingleWinner(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	first := NewElection(store, "/test/", "node-a", time.Minute, time.Second)
	second := NewElection(store, "/test/", "node-b", time.Minute, time.Second)

	won, err := first.Campaign(ctx)
	require.NoError(t, err)
	assert.True(t, won)
	assert.True(t, first.IsLeader())

	won, err = second.Campaign(ctx)
	require.NoError(t, err)
	assert.False(t, won)
	assert.False(t, second.IsLeader())
	assert.Equal(t, "node-a", second.Leader())

	// the leader keeps the key on the next round
	won, err = first.Campaign(ctx)
	require.NoError(t, err)
	assert.True(t, won)
}

func TestElectionHandoff(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	store := NewMemoryStore()
	store.SetClock(clock.Now)
	defer func() { require.NoError(t, store.Close()) }()

	ttl := 15 * time.Second
	first := NewElection(store, "/test/", "node-a", ttl, ttl/3)
	second := NewElection(store, "/test/", "node-b", ttl, ttl/3)

	tests := []struct {
		name       string
		run        func(t *testing.T)
		wantLeader string
	}{
		{
			name: "first node wins",
			run: func(t *testing.T) {
				won, err := first.Campaign(ctx)
				require.NoError(t, err)
				require.True(t, won)
			},
			wantLeader: "node-a",
		},
		{
			name: "second node waits while the leader is alive",
			run: func(t *testing.T) {
				won, err := second.Campaign(ctx)
				require.NoError(t, err)
				require.False(t, won)
			},
			wantLeader: "node-a",
		},
		{
			name: "resign hands over immediately",
			run: func(t *testing.T) {
				require.NoError(t, first.Resign(ctx))
				won, err := second.Campaign(ctx)
				require.NoError(t, err)
				require.True(t, won)
			},
			wantLeader: "node-b",
		},
		{
			name: "expired lease elects the other node",
			run: func(t *testing.T) {
				clock.Advance(2 * ttl) // node-b stopped heartbeating
				won, err := first.Campaign(ctx)
				require.NoError(t, err)
				require.True(t, won)
			},
			wantLeader: "node-a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t)
			kv, err := store.Get(ctx, first.Key())
			require.NoError(t, err)
			assert.Equal(t, tt.wantLeader, string(kv.Value))
		})
	}

	// the node that lost its lease must notice on its next round
	won, err := second.Campaign(ctx)
	require.NoError(t, err)
	assert.False(t, won)
	assert.False(t, second.IsLeader())
}

func TestCoordinatorRebalanceOnce(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	membership := testMembership(store, "node-a", time.Minute)
	require.NoError(t, membership.Join(ctx))
	require.NoError(t, testMembership(store, "node-b", time.Minute).Join(ctx))

	assigner := NewAssigner(store, "/test/", 8)
	election := NewElection(store, "/test/", "node-a", time.Minute, time.Second)
	coordinator := NewCoordinator(election, membership, assigner, 8, time.Second)

	// a follower must not write anything
	require.NoError(t, coordinator.RebalanceOnce(ctx))
	assignments, err := assigner.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, assignments)

	won, err := election.Campaign(ctx)
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, coordinator.RebalanceOnce(ctx))

	assignments, err = assigner.List(ctx)
	require.NoError(t, err)
	require.Len(t, assignments, 8)
	counts := map[string]int{}
	for _, assignment := range assignments {
		counts[assignment.Owner]++
		assert.Positive(t, assignment.Epoch)
	}
	assert.Equal(t, map[string]int{"node-a": 4, "node-b": 4}, counts)

	// a second round with the same members changes nothing
	before := assignments
	require.NoError(t, coordinator.RebalanceOnce(ctx))
	after, err := assigner.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}
