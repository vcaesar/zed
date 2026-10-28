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
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/errors"
)

// Election is a lease based leader election on `<prefix>leader`. The winner
// is whoever manages to create the key; it stays leader as long as it keeps
// the lease alive. When the process dies the lease expires, the key is
// removed and the next campaign round elects somebody else.
//
// The leader is only a convenience: it is the node that computes and writes
// the partition assignment. Correctness of ownership does not depend on
// there being exactly one leader, every assignment write is a
// compare-and-swap on the partition key.
type Election struct {
	store    Store
	key      string
	nodeID   string
	ttl      time.Duration
	interval time.Duration

	mu      sync.RWMutex
	leader  string
	lease   LeaseID
	renewed time.Time // start of the last successful grant or renewal
}

// NewElection builds an election for nodeID over `<prefix>leader`.
func NewElection(store Store, prefix, nodeID string, ttl, interval time.Duration) *Election {
	return &Election{
		store:    store,
		key:      prefix + "leader",
		nodeID:   nodeID,
		ttl:      ttl,
		interval: interval,
	}
}

// Key is the key the election is held on.
func (e *Election) Key() string {
	return e.key
}

// Leader returns the node id currently believed to hold the leadership,
// empty when it is unknown.
func (e *Election) Leader() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.leader
}

// IsLeader reports whether the local node last won the election and its
// lease cannot have expired yet: when renewals keep failing, another node
// may already have taken the key.
func (e *Election) IsLeader() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.leader == e.nodeID && e.lease != NoLease && time.Since(e.renewed) < e.ttl
}

func (e *Election) set(leader string, lease LeaseID) {
	e.setRenewed(leader, lease, time.Time{})
}

func (e *Election) setRenewed(leader string, lease LeaseID, renewed time.Time) {
	e.mu.Lock()
	e.leader = leader
	e.lease = lease
	e.renewed = renewed
	e.mu.Unlock()
}

// Campaign runs one election round: renew leadership when we hold it, else
// try to take the key. It reports whether the local node is leader after
// the round.
func (e *Election) Campaign(ctx context.Context) (bool, error) {
	e.mu.RLock()
	lease, leading := e.lease, e.leader == e.nodeID
	e.mu.RUnlock()

	if leading && lease != NoLease {
		start := time.Now()
		err := e.store.KeepAlive(ctx, lease)
		if err == nil {
			e.mu.Lock()
			e.renewed = start
			e.mu.Unlock()
			return true, nil
		}
		if !errors.Is(err, errors.ErrKeyNotFound) {
			return false, err
		}
		log.Warn().Str("node", e.nodeID).Msg("cluster: leader lease lost")
		e.set("", NoLease)
	}

	start := time.Now()
	lease, err := e.store.Grant(ctx, e.ttl)
	if err != nil {
		return false, err
	}
	_, won, err := e.store.Create(ctx, e.key, []byte(e.nodeID), lease)
	if err != nil {
		e.revoke(ctx, lease)
		return false, err
	}
	if won {
		e.setRenewed(e.nodeID, lease, start)
		log.Info().Str("node", e.nodeID).Msg("cluster: elected leader")
		return true, nil
	}
	// somebody else holds the key, drop the lease we did not use
	e.revoke(ctx, lease)

	kv, err := e.store.Get(ctx, e.key)
	if err != nil {
		if errors.Is(err, errors.ErrKeyNotFound) {
			// the leader went away between Create and Get, retry next round
			e.set("", NoLease)
			return false, nil
		}
		return false, err
	}
	e.set(string(kv.Value), NoLease)
	return false, nil
}

// Resign gives up leadership so another node can take over immediately
// instead of waiting for the lease to expire.
func (e *Election) Resign(ctx context.Context) error {
	e.mu.Lock()
	lease, leading := e.lease, e.leader == e.nodeID
	e.leader, e.lease = "", NoLease
	e.mu.Unlock()

	if !leading {
		return nil
	}
	if err := e.store.Delete(ctx, e.key); err != nil {
		return err
	}
	if lease == NoLease {
		return nil
	}
	return e.store.Revoke(ctx, lease)
}

// Run campaigns every interval until ctx is done.
func (e *Election) Run(ctx context.Context) {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		if _, err := e.Campaign(ctx); err != nil && ctx.Err() == nil {
			log.Error().Err(err).Msg("cluster: election round failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *Election) revoke(ctx context.Context, lease LeaseID) {
	if err := e.store.Revoke(ctx, lease); err != nil {
		log.Warn().Err(err).Msg("cluster: revoke unused election lease")
	}
}

// Coordinator keeps the partition map in shape. Only the leader writes, the
// others just watch membership so they can take over without delay.
type Coordinator struct {
	election   *Election
	membership *Membership
	assigner   *Assigner
	partitions int
	replicas   int
	interval   time.Duration
}

// NewCoordinator wires the pieces the rebalance loop needs. replicas is the
// number of warm read replicas kept per partition.
func NewCoordinator(election *Election, membership *Membership, assigner *Assigner, partitions, replicas int, interval time.Duration) *Coordinator {
	if partitions <= 0 {
		partitions = DefaultPartitions
	}
	return &Coordinator{
		election:   election,
		membership: membership,
		assigner:   assigner,
		partitions: partitions,
		replicas:   max(replicas, 0),
		interval:   interval,
	}
}

// RebalanceOnce recomputes ownership from the live member list, writes the
// partitions that must move, then the replica map. Orphaned partitions go to
// a warm replica first. It is a no-op on non-leaders.
func (c *Coordinator) RebalanceOnce(ctx context.Context) error {
	if !c.election.IsLeader() {
		return nil
	}
	nodes, err := c.membership.ListNodes(ctx)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return nil
	}
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}

	current, err := c.assigner.List(ctx)
	if err != nil {
		return err
	}
	owners := make(map[int]string, len(current))
	for partition, assignment := range current {
		owners[partition] = assignment.Owner
	}

	replicas, rev, err := c.assigner.ListReplicas(ctx)
	if err != nil {
		return err
	}

	changes := RebalanceWarm(owners, replicas, ids, c.partitions)
	if len(changes) > 0 {
		if err := c.assigner.Apply(ctx, current, changes); err != nil {
			return err
		}
	}
	// a change skipped by Apply is fixed by the next round, replicas follow
	for partition, owner := range changes {
		owners[partition] = owner
	}

	want := PlanReplicas(owners, ids, c.replicas)
	if maps.EqualFunc(want, replicas, slices.Equal) {
		return nil
	}
	ok, err := c.assigner.SetReplicas(ctx, want, rev)
	if err != nil {
		return err
	}
	if !ok {
		log.Warn().Msg("cluster: replica map changed concurrently, skipped")
	}
	return nil
}

// Run rebalances on every membership change and on a slow timer, which
// covers missed watch events after a reconnect.
func (c *Coordinator) Run(ctx context.Context) {
	events, err := c.membership.Watch(ctx)
	if err != nil {
		log.Error().Err(err).Msg("cluster: watch membership")
		events = nil
	}
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		if err := c.RebalanceOnce(ctx); err != nil && ctx.Err() == nil {
			log.Error().Err(err).Msg("cluster: rebalance failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case _, ok := <-events:
			if !ok {
				events = nil
			}
		}
	}
}
