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

	"github.com/zincsearch/zincsearch/pkg/config"
)

// startTestCluster brings the package global up on an in-memory store and
// tears it down again after the test.
func startTestCluster(t *testing.T, nodeID, addr string, partitions int) *MemoryStore {
	t.Helper()
	previous := config.Global.Cluster
	config.Global.Cluster.Name = "test-cluster"
	config.Global.Cluster.NodeID = nodeID
	config.Global.Cluster.AdvertiseAddr = addr
	config.Global.Cluster.Partitions = partitions
	config.Global.Cluster.HeartbeatTTL = 15 * time.Second
	config.Global.Cluster.HeartbeatInterval = 100 * time.Millisecond

	store := NewMemoryStore()
	require.NoError(t, StartWith(context.Background(), store))
	t.Cleanup(func() {
		Stop()
		config.Global.Cluster = previous
	})
	return store
}

func TestClusterDisabled(t *testing.T) {
	require.False(t, Enabled())

	assert.Nil(t, Local())
	assert.True(t, IsLocalOwner("anything"))
	assert.NoError(t, CheckEpoch("anything", 42))
	assert.True(t, IsLeader())
	assert.Empty(t, Assignments())

	_, _, ok := Owner("anything")
	assert.False(t, ok)
	_, ok = NodeAddr("other")
	assert.False(t, ok)

	nodes := Nodes()
	require.Len(t, nodes, 1, "a single process must report itself as the whole cluster")
	assert.True(t, nodes[0].Alive)
	assert.True(t, nodes[0].IsLeader)
}

func TestClusterStartStop(t *testing.T) {
	store := startTestCluster(t, "node-a", "node-a:4080", 4)

	require.True(t, Enabled())
	local := Local()
	require.NotNil(t, local)
	assert.Equal(t, "node-a", local.ID)
	assert.Equal(t, "http://node-a:4080", local.Addr, "a bare address gets an http scheme")

	addr, ok := NodeAddr("node-a")
	assert.True(t, ok)
	assert.Equal(t, "http://node-a:4080", addr)

	nodes := Nodes()
	require.Len(t, nodes, 1)
	assert.Equal(t, "node-a", nodes[0].ID)

	// the only member becomes leader and assigns every partition to itself
	require.Eventually(t, IsLeader, 3*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return len(Assignments()) == 4 }, 3*time.Second, 10*time.Millisecond)

	owner, epoch, ok := Owner("logs-2024")
	require.True(t, ok)
	assert.Equal(t, "node-a", owner)
	assert.Positive(t, epoch)
	assert.True(t, IsLocalOwner("logs-2024"))
	assert.NoError(t, CheckEpoch("logs-2024", epoch))
	assert.Error(t, CheckEpoch("logs-2024", epoch-1))

	status := Nodes()
	require.Len(t, status, 1)
	assert.Equal(t, 4, status[0].Partitions)
	assert.True(t, status[0].IsLeader)

	Stop()
	assert.False(t, Enabled())
	// the membership record is gone, peers stop routing here
	nodesAfter, err := NewMembership(store, KeyPrefix(), Node{ID: "probe"}, time.Minute, time.Second).ListNodes(context.Background())
	require.NoError(t, err)
	assert.Empty(t, nodesAfter)
}

func TestClusterSecondNodeSharesPartitions(t *testing.T) {
	store := startTestCluster(t, "node-a", "node-a:4080", 4)
	require.Eventually(t, func() bool { return len(Assignments()) == 4 }, 3*time.Second, 10*time.Millisecond)

	// a second process joins the same store
	ctx := context.Background()
	peer := NewMembership(store, KeyPrefix(), Node{ID: "node-b", Addr: "http://node-b:4080"}, time.Minute, time.Second)
	require.NoError(t, peer.Join(ctx))

	require.Eventually(t, func() bool {
		counts := map[string]int{}
		for _, assignment := range Assignments() {
			counts[assignment.Owner]++
		}
		return counts["node-a"] == 2 && counts["node-b"] == 2
	}, 5*time.Second, 20*time.Millisecond, "the leader must hand half the partitions to the new node")

	addr, ok := NodeAddr("node-b")
	require.True(t, ok)
	assert.Equal(t, "http://node-b:4080", addr)

	nodes := Nodes()
	require.Len(t, nodes, 2)
	for _, node := range nodes {
		assert.Equal(t, 2, node.Partitions)
		assert.Equal(t, node.ID == "node-a", node.IsLeader)
	}
}

func TestKeyPrefix(t *testing.T) {
	previous := config.Global
	t.Cleanup(func() { config.Global = previous })

	config.Global.Etcd.Prefix = "/zinc"
	config.Global.Cluster.Name = "prod"
	assert.Equal(t, "/zinc/cluster/prod/", KeyPrefix())

	config.Global.Etcd.Prefix = "/zinc/"
	assert.Equal(t, "/zinc/cluster/prod/", KeyPrefix())

	config.Global.Etcd.Prefix = ""
	config.Global.Cluster.Name = ""
	assert.Equal(t, "/zinc/cluster/zincsearch/", KeyPrefix())
}
