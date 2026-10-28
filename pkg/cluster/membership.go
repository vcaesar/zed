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
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/errors"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// Node is one zedsearch process in the cluster.
type Node struct {
	ID        string `json:"id"`
	Addr      string `json:"addr"`
	StartedAt int64  `json:"started_at"` // unix milliseconds
}

// Membership keeps `<prefix>/nodes/<id>` alive under a lease. When the
// process dies the lease expires and the key disappears, which is how the
// rest of the cluster learns the node is gone.
type Membership struct {
	store    Store
	prefix   string // key prefix, ends with '/'
	local    Node
	ttl      time.Duration
	interval time.Duration

	mu    sync.Mutex
	lease LeaseID
}

// NewMembership builds membership for local under prefix.
func NewMembership(store Store, prefix string, local Node, ttl, interval time.Duration) *Membership {
	return &Membership{
		store:    store,
		prefix:   prefix,
		local:    local,
		ttl:      ttl,
		interval: interval,
	}
}

// NodesPrefix is the key prefix every node record lives under.
func (m *Membership) NodesPrefix() string {
	return m.prefix + "nodes/"
}

func (m *Membership) nodeKey(id string) string {
	return m.NodesPrefix() + id
}

// Lease returns the lease currently backing the local node record.
func (m *Membership) Lease() LeaseID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lease
}

// Join grants a lease and registers the local node under it.
func (m *Membership) Join(ctx context.Context) error {
	lease, err := m.store.Grant(ctx, m.ttl)
	if err != nil {
		return err
	}
	value, err := json.Marshal(m.local)
	if err != nil {
		return err
	}
	if _, err = m.store.Put(ctx, m.nodeKey(m.local.ID), value, lease); err != nil {
		return err
	}
	m.mu.Lock()
	m.lease = lease
	m.mu.Unlock()
	return nil
}

// KeepAlive refreshes the lease once. A lease that the store no longer knows
// about (expired during a network partition) is replaced by re-joining, so a
// node that comes back always reappears in the member list.
func (m *Membership) KeepAlive(ctx context.Context) error {
	lease := m.Lease()
	if lease == NoLease {
		return m.Join(ctx)
	}
	err := m.store.KeepAlive(ctx, lease)
	if errors.Is(err, errors.ErrKeyNotFound) {
		log.Warn().Str("node", m.local.ID).Msg("cluster: lease expired, re-joining")
		return m.Join(ctx)
	}
	return err
}

// Leave revokes the lease and removes the node record.
func (m *Membership) Leave(ctx context.Context) error {
	m.mu.Lock()
	lease := m.lease
	m.lease = NoLease
	m.mu.Unlock()

	if err := m.store.Delete(ctx, m.nodeKey(m.local.ID)); err != nil {
		return err
	}
	if lease == NoLease {
		return nil
	}
	return m.store.Revoke(ctx, lease)
}

// ListNodes returns every live node, sorted by id so every node computes the
// same rebalance result from the same input.
func (m *Membership) ListNodes(ctx context.Context) ([]Node, error) {
	kvs, err := m.store.List(ctx, m.NodesPrefix())
	if err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(kvs))
	for _, kv := range kvs {
		var node Node
		if err := json.Unmarshal(kv.Value, &node); err != nil {
			log.Error().Err(err).Str("key", kv.Key).Msg("cluster: bad node record")
			continue
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes, nil
}

// Watch streams membership changes until ctx is done.
func (m *Membership) Watch(ctx context.Context) (<-chan Event, error) {
	return m.store.Watch(ctx, m.NodesPrefix())
}
