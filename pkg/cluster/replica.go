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

	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/errors"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// The replica map (partition -> read replicas) lives in one key next to the
// assignments, not inside them: a replica change must not bump the ownership
// epoch, or every write in flight on the owner would be fenced for nothing.

// ReplicasKey is the key holding the replica map.
func (a *Assigner) ReplicasKey() string {
	return a.prefix + "replicas"
}

// ListReplicas returns the replica map and the revision it was read at, 0
// when it was never written.
func (a *Assigner) ListReplicas(ctx context.Context) (map[int][]string, int64, error) {
	kv, err := a.store.Get(ctx, a.ReplicasKey())
	if errors.Is(err, errors.ErrKeyNotFound) {
		return map[int][]string{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	replicas, err := decodeReplicas(kv.Value)
	if err != nil {
		return nil, 0, err
	}
	return replicas, kv.Revision, nil
}

// SetReplicas writes the replica map when it is still at rev.
func (a *Assigner) SetReplicas(ctx context.Context, replicas map[int][]string, rev int64) (bool, error) {
	value, err := json.Marshal(replicas)
	if err != nil {
		return false, err
	}
	_, ok, err := a.store.CompareAndSwap(ctx, a.ReplicasKey(), value, rev, NoLease)
	return ok, err
}

func decodeReplicas(data []byte) (map[int][]string, error) {
	replicas := make(map[int][]string)
	if err := json.Unmarshal(data, &replicas); err != nil {
		return nil, err
	}
	return replicas, nil
}

// PlanReplicas picks n read replicas for every owned partition among the
// live nodes, never the owner itself. The start node rotates with the
// partition number, so the replicas of one owner spread over all the others
// instead of piling onto its ring successor.
func PlanReplicas(owners map[int]string, nodes []string, n int) map[int][]string {
	out := make(map[int][]string, len(owners))
	if n <= 0 {
		return out
	}
	sorted := append([]string(nil), nodes...)
	sort.Strings(sorted)

	others := make([]string, 0, len(sorted))
	for partition, owner := range owners {
		others = others[:0]
		for _, node := range sorted {
			if node != owner {
				others = append(others, node)
			}
		}
		count := min(n, len(others))
		if count == 0 {
			continue
		}
		replicas := make([]string, 0, count)
		start := partition % len(others)
		for i := 0; i < count; i++ {
			replicas = append(replicas, others[(start+i)%len(others)])
		}
		out[partition] = replicas
	}
	return out
}

func replicaCount() int {
	return max(config.Global.Cluster.Replicas, 0)
}
