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
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/config"
)

// NodeStatus is one row of `GET /api/cluster/nodes`.
type NodeStatus struct {
	ID         string `json:"id"`
	Addr       string `json:"addr"`
	Alive      bool   `json:"alive"`
	Partitions int    `json:"partitions"`
	IsLeader   bool   `json:"is_leader"`
}

// node holds everything a running cluster member needs. Nil when
// clustering is disabled, which is the single node build.
type node struct {
	store      Store
	membership *Membership
	election   *Election
	assigner   *Assigner
	fence      *Fence
	local      Node

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

var (
	mu      sync.RWMutex
	current *node
)

// Enabled reports whether this process participates in a cluster.
func Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current != nil
}

func get() *node {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Start joins the cluster. It is a no-op when clustering is disabled, so
// callers can call it unconditionally.
func Start(ctx context.Context) error {
	if !config.Global.Cluster.Enable {
		return nil
	}
	store, err := NewEtcdStore()
	if err != nil {
		return err
	}
	if err := start(ctx, store); err != nil {
		if closeErr := store.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("cluster: close store")
		}
		return err
	}
	return nil
}

// StartWith joins the cluster using a caller provided store. Tests use it
// with a MemoryStore, production goes through Start.
func StartWith(ctx context.Context, store Store) error {
	return start(ctx, store)
}

// start joins the cluster using store, tests call it with a MemoryStore.
func start(ctx context.Context, store Store) error {
	mu.Lock()
	if current != nil {
		mu.Unlock()
		return nil
	}
	mu.Unlock()

	prefix := keyPrefix()
	ttl, interval := heartbeat()
	partitions := partitionCount()

	local := Node{
		ID:        nodeID(),
		Addr:      advertiseAddr(),
		StartedAt: time.Now().UnixMilli(),
	}
	membership := NewMembership(store, prefix, local, ttl, interval)
	assigner := NewAssigner(store, prefix, partitions)
	election := NewElection(store, prefix, local.ID, ttl, interval)
	fence := NewFence(assigner, local.ID, partitions)

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	if err := membership.Join(runCtx); err != nil {
		cancel()
		return err
	}
	if err := fence.Refresh(runCtx); err != nil {
		cancel()
		return err
	}

	n := &node{
		store:      store,
		membership: membership,
		election:   election,
		assigner:   assigner,
		fence:      fence,
		local:      local,
		cancel:     cancel,
	}
	coordinator := NewCoordinator(election, membership, assigner, partitions, replicaCount(), interval)

	n.wg.Add(5)
	go func() { defer n.wg.Done(); n.heartbeatLoop(runCtx, interval) }()
	go func() { defer n.wg.Done(); election.Run(runCtx) }()
	go func() { defer n.wg.Done(); coordinator.Run(runCtx) }()
	go func() { defer n.wg.Done(); fence.Run(runCtx, interval) }()
	go func() { defer n.wg.Done(); membership.Run(runCtx, interval) }()

	mu.Lock()
	current = n
	mu.Unlock()

	log.Info().Str("node", local.ID).Str("addr", local.Addr).Int("partitions", partitions).
		Msg("cluster: joined")
	return nil
}

// Stop leaves the cluster: resign leadership, drop the membership record so
// peers stop routing here, then close the store.
func Stop() {
	mu.Lock()
	n := current
	current = nil
	mu.Unlock()
	if n == nil {
		return
	}

	// stop the loops first, a heartbeat or campaign after Leave / Resign
	// would register this node again
	n.cancel()
	n.wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := n.election.Resign(ctx); err != nil {
		log.Error().Err(err).Msg("cluster: resign")
	}
	if err := n.membership.Leave(ctx); err != nil {
		log.Error().Err(err).Msg("cluster: leave")
	}
	if err := n.store.Close(); err != nil {
		log.Error().Err(err).Msg("cluster: close store")
	}
	log.Info().Str("node", n.local.ID).Msg("cluster: left")
}

func (n *node) heartbeatLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := n.membership.KeepAlive(ctx); err != nil && ctx.Err() == nil {
				log.Error().Err(err).Msg("cluster: heartbeat failed")
			}
		}
	}
}

// Local returns the local node record, nil when clustering is disabled.
func Local() *Node {
	n := get()
	if n == nil {
		return nil
	}
	local := n.local
	return &local
}

// Nodes lists the cluster members. With clustering disabled it reports the
// single local process, so callers do not need a special case.
func Nodes() []NodeStatus {
	n := get()
	if n == nil {
		return []NodeStatus{{
			ID:         nodeID(),
			Addr:       advertiseAddr(),
			Alive:      true,
			Partitions: partitionCount(),
			IsLeader:   true,
		}}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	nodes, err := n.membership.ListNodes(ctx)
	if err != nil {
		log.Error().Err(err).Msg("cluster: list nodes")
		nodes = []Node{n.local}
	}

	counts := n.fence.PartitionCounts()
	leader := n.election.Leader()
	out := make([]NodeStatus, 0, len(nodes))
	for _, member := range nodes {
		out = append(out, NodeStatus{
			ID:         member.ID,
			Addr:       member.Addr,
			Alive:      true, // the record only exists while the lease is alive
			Partitions: counts[member.ID],
			IsLeader:   member.ID == leader,
		})
	}
	return out
}

// NodeAddr returns the advertised address of a node id. It is answered from
// the member cache; a miss (a node that just joined) falls back to one read.
func NodeAddr(id string) (string, bool) {
	n := get()
	if n == nil {
		return "", false
	}
	if id == n.local.ID {
		return n.local.Addr, n.local.Addr != ""
	}
	if addr, ok := n.membership.Addr(id); ok {
		return addr, addr != ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := n.membership.Refresh(ctx); err != nil {
		log.Error().Err(err).Msg("cluster: list nodes")
		return "", false
	}
	addr, ok := n.membership.Addr(id)
	return addr, ok && addr != ""
}

// Owner returns the node owning indexName and the epoch of that ownership.
// ok is false when clustering is off or the partition is unassigned.
func Owner(indexName string) (string, int64, bool) {
	n := get()
	if n == nil {
		return "", 0, false
	}
	return n.fence.Owner(indexName)
}

// IsLocalOwner reports whether this node may write indexName.
func IsLocalOwner(indexName string) bool {
	n := get()
	if n == nil {
		return true
	}
	return n.fence.IsLocalOwner(indexName)
}

// Replicas returns the nodes keeping a warm read replica of indexName,
// empty when clustering is off or `zed_cluster_replicas` is 0.
func Replicas(indexName string) []string {
	n := get()
	if n == nil {
		return nil
	}
	return n.fence.Replicas(indexName)
}

// IsLocalReplica reports whether this node is a read replica of indexName.
// Only indexes every node can open read-only (cold, object storage) may be
// served from a replica; that check is left to the caller.
func IsLocalReplica(indexName string) bool {
	n := get()
	if n == nil {
		return false
	}
	return n.fence.IsLocalReplica(indexName)
}

// CheckEpoch fails when the partition of indexName moved since epoch was
// taken. Always nil with clustering disabled.
func CheckEpoch(indexName string, epoch int64) error {
	n := get()
	if n == nil {
		return nil
	}
	return n.fence.CheckEpoch(indexName, epoch)
}

// IsLeader reports whether this node currently holds the leader lock.
func IsLeader() bool {
	n := get()
	if n == nil {
		return true
	}
	return n.election.IsLeader()
}

// Assignments returns the ownership map, empty when clustering is off.
func Assignments() map[int]Assignment {
	n := get()
	if n == nil {
		return map[int]Assignment{}
	}
	return n.fence.Assignments()
}

// KeyPrefix is the etcd key prefix every cluster key of this deployment
// lives under.
func KeyPrefix() string {
	return keyPrefix()
}

// keyPrefix scopes every cluster key by etcd prefix and cluster name, so
// two clusters can share one etcd.
func keyPrefix() string {
	prefix := strings.TrimSuffix(config.Global.Etcd.Prefix, "/")
	if prefix == "" {
		prefix = "/zed"
	}
	name := config.Global.Cluster.Name
	if name == "" {
		name = "zedsearch"
	}
	return prefix + "/cluster/" + name + "/"
}

// nodeID defaults to the hostname, which is stable for a pod or a host.
func nodeID() string {
	if id := config.Global.Cluster.NodeID; id != "" {
		return id
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		log.Warn().Err(err).Msg("cluster: hostname unavailable, using 'node'")
		return "node"
	}
	return host
}

func advertiseAddr() string {
	addr := config.Global.Cluster.AdvertiseAddr
	if addr == "" {
		return ""
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	return strings.TrimSuffix(addr, "/")
}

func heartbeat() (ttl, interval time.Duration) {
	ttl = config.Global.Cluster.HeartbeatTTL
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	interval = config.Global.Cluster.HeartbeatInterval
	if interval <= 0 || interval >= ttl {
		interval = ttl / 3
	}
	return ttl, interval
}
