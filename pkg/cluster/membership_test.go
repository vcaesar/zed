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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock drives lease expiry without sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func testMembership(store Store, id string, ttl time.Duration) *Membership {
	return NewMembership(store, "/test/", Node{ID: id, Addr: "http://" + id + ":4080"}, ttl, ttl/3)
}

func TestMembershipJoinLeave(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	first := testMembership(store, "node-a", time.Minute)
	second := testMembership(store, "node-b", time.Minute)
	require.NoError(t, first.Join(ctx))
	require.NoError(t, second.Join(ctx))

	nodes, err := first.ListNodes(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	assert.Equal(t, "node-a", nodes[0].ID)
	assert.Equal(t, "node-b", nodes[1].ID)
	assert.Equal(t, "http://node-b:4080", nodes[1].Addr)

	require.NoError(t, second.Leave(ctx))
	nodes, err = first.ListNodes(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "node-a", nodes[0].ID)
	assert.Equal(t, NoLease, second.Lease())
}

func TestMembershipTTLExpiry(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	store := NewMemoryStore()
	store.SetClock(clock.Now)
	defer func() { require.NoError(t, store.Close()) }()

	ttl := 15 * time.Second
	alive := testMembership(store, "node-alive", ttl)
	dead := testMembership(store, "node-dead", ttl)
	require.NoError(t, alive.Join(ctx))
	require.NoError(t, dead.Join(ctx))

	tests := []struct {
		name      string
		advance   time.Duration
		keepAlive bool
		want      []string
	}{
		{name: "both alive", advance: 5 * time.Second, keepAlive: true, want: []string{"node-alive", "node-dead"}},
		{name: "one heartbeat missed", advance: 10 * time.Second, keepAlive: true, want: []string{"node-alive", "node-dead"}},
		{name: "ttl passed without heartbeat", advance: 20 * time.Second, keepAlive: false, want: []string{"node-alive"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Advance(tt.advance)
			if tt.keepAlive {
				require.NoError(t, dead.KeepAlive(ctx))
			}
			require.NoError(t, alive.KeepAlive(ctx))

			nodes, err := alive.ListNodes(ctx)
			require.NoError(t, err)
			ids := make([]string, 0, len(nodes))
			for _, node := range nodes {
				ids = append(ids, node.ID)
			}
			assert.Equal(t, tt.want, ids)
		})
	}
}

func TestMembershipKeepAliveRejoinsAfterExpiry(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	store := NewMemoryStore()
	store.SetClock(clock.Now)
	defer func() { require.NoError(t, store.Close()) }()

	member := testMembership(store, "node-a", 15*time.Second)
	require.NoError(t, member.Join(ctx))
	lease := member.Lease()

	// the process was paused past its ttl, the record is gone
	clock.Advance(30 * time.Second)
	nodes, err := member.ListNodes(ctx)
	require.NoError(t, err)
	assert.Empty(t, nodes)

	// the next heartbeat must bring the node back
	require.NoError(t, member.KeepAlive(ctx))
	nodes, err = member.ListNodes(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.NotEqual(t, lease, member.Lease())
}

func TestMembershipWatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewMemoryStore()
	defer func() { require.NoError(t, store.Close()) }()

	member := testMembership(store, "node-a", time.Minute)
	events, err := member.Watch(ctx)
	require.NoError(t, err)
	require.NoError(t, member.Join(ctx))

	select {
	case event := <-events:
		assert.Equal(t, EventPut, event.Type)
		assert.Equal(t, "/test/nodes/node-a", event.KV.Key)
	case <-time.After(time.Second):
		t.Fatal("no membership event")
	}
}
