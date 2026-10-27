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
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch/pkg/config"
)

// etcdTestStore dials the etcd in ZINC_ETCD_ENDPOINTS, or skips. Every run
// works under its own prefix, so concurrent runs do not collide.
func etcdTestStore(t *testing.T) (*EtcdStore, string) {
	t.Helper()
	endpoints := os.Getenv("ZINC_ETCD_ENDPOINTS")
	if endpoints == "" {
		t.Skip("set ZINC_ETCD_ENDPOINTS to run the etcd integration test")
	}

	previous := config.Global.Etcd
	config.Global.Etcd.Endpoints = strings.Split(endpoints, ",")
	config.Global.Etcd.Username = os.Getenv("ZINC_ETCD_USERNAME")
	config.Global.Etcd.Password = os.Getenv("ZINC_ETCD_PASSWORD")

	store, err := NewEtcdStore()
	require.NoError(t, err)

	prefix := "/zinc-test/" + strconv.FormatInt(time.Now().UnixNano(), 10) + "/"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		kvs, err := store.List(ctx, prefix)
		if err == nil {
			for _, kv := range kvs {
				if err := store.Delete(ctx, kv.Key); err != nil {
					t.Logf("cleanup %s: %v", kv.Key, err)
				}
			}
		}
		assert.NoError(t, store.Close())
		config.Global.Etcd = previous
	})
	return store, prefix
}

func TestEtcdStoreIntegration(t *testing.T) {
	store, prefix := etcdTestStore(t)
	ctx := context.Background()

	t.Run("membership", func(t *testing.T) {
		first := NewMembership(store, prefix, Node{ID: "node-a", Addr: "http://a:4080"}, 5*time.Second, time.Second)
		second := NewMembership(store, prefix, Node{ID: "node-b", Addr: "http://b:4080"}, 5*time.Second, time.Second)
		require.NoError(t, first.Join(ctx))
		require.NoError(t, second.Join(ctx))
		defer func() { assert.NoError(t, first.Leave(ctx)) }()

		nodes, err := first.ListNodes(ctx)
		require.NoError(t, err)
		require.Len(t, nodes, 2)

		require.NoError(t, first.KeepAlive(ctx))
		require.NoError(t, second.Leave(ctx))

		nodes, err = first.ListNodes(ctx)
		require.NoError(t, err)
		require.Len(t, nodes, 1)
		assert.Equal(t, "node-a", nodes[0].ID)
	})

	t.Run("election", func(t *testing.T) {
		first := NewElection(store, prefix, "node-a", 5*time.Second, time.Second)
		second := NewElection(store, prefix, "node-b", 5*time.Second, time.Second)

		won, err := first.Campaign(ctx)
		require.NoError(t, err)
		require.True(t, won)

		won, err = second.Campaign(ctx)
		require.NoError(t, err)
		assert.False(t, won)
		assert.Equal(t, "node-a", second.Leader())

		require.NoError(t, first.Resign(ctx))
		won, err = second.Campaign(ctx)
		require.NoError(t, err)
		assert.True(t, won)
		require.NoError(t, second.Resign(ctx))
	})

	t.Run("assignment epochs grow", func(t *testing.T) {
		assigner := NewAssigner(store, prefix, 8)
		first, ok, err := assigner.Set(ctx, 3, "node-a", 0)
		require.NoError(t, err)
		require.True(t, ok)

		second, ok, err := assigner.Set(ctx, 3, "node-b", first.Epoch)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Greater(t, second.Epoch, first.Epoch)

		_, ok, err = assigner.Set(ctx, 3, "node-c", first.Epoch)
		require.NoError(t, err)
		assert.False(t, ok, "a stale epoch must not win")

		assignments, err := assigner.List(ctx)
		require.NoError(t, err)
		assert.Equal(t, "node-b", assignments[3].Owner)
	})

	t.Run("fence follows the watch", func(t *testing.T) {
		watchCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		assigner := NewAssigner(store, prefix, 8)
		fence := NewFence(assigner, "node-a", 8)
		go fence.Run(watchCtx, time.Minute)

		const index = "etcd-index"
		partition := PartitionOfN(index, 8)
		current, err := assigner.List(ctx)
		require.NoError(t, err)
		assignment, ok, err := assigner.Set(ctx, partition, "node-a", current[partition].Epoch)
		require.NoError(t, err)
		require.True(t, ok)

		require.Eventually(t, func() bool {
			owner, epoch, ok := fence.Owner(index)
			return ok && owner == "node-a" && epoch == assignment.Epoch
		}, 10*time.Second, 50*time.Millisecond)
		assert.NoError(t, fence.CheckEpoch(index, assignment.Epoch))
		assert.Error(t, fence.CheckEpoch(index, assignment.Epoch-1))
	})
}
