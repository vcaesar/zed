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
	"hash/fnv"
	"sort"
	"strconv"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// DefaultPartitions is used when the config leaves the count unset.
const DefaultPartitions = 256

// Assignment names the single node that owns a partition.
//
// Epoch is the store revision of the assignment key. It is never written by
// us, it is filled in from the revision the store reports, so it increases
// on every handoff and can be used as a fencing token.
type Assignment struct {
	Partition int    `json:"partition"`
	Owner     string `json:"owner"`
	Epoch     int64  `json:"epoch"`
}

// assignmentValue is what is actually stored, the epoch is the key revision.
type assignmentValue struct {
	Owner string `json:"owner"`
}

// PartitionOf maps an index name to its partition using the configured
// partition count. The mapping is a pure function of the name, so every node
// computes the same partition without coordination.
func PartitionOf(indexName string) int {
	return PartitionOfN(indexName, partitionCount())
}

// PartitionOfN maps an index name onto `partitions` buckets.
func PartitionOfN(indexName string, partitions int) int {
	if partitions <= 0 {
		partitions = DefaultPartitions
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(indexName)) // hash.Hash never fails
	return int(h.Sum32() % uint32(partitions))
}

func partitionCount() int {
	if config.Global.Cluster.Partitions > 0 {
		return config.Global.Cluster.Partitions
	}
	return DefaultPartitions
}

// Assigner reads and writes the partition ownership map.
type Assigner struct {
	store      Store
	prefix     string // key prefix, ends with '/'
	partitions int
}

// NewAssigner builds an assigner over `<prefix>assign/`.
func NewAssigner(store Store, prefix string, partitions int) *Assigner {
	if partitions <= 0 {
		partitions = DefaultPartitions
	}
	return &Assigner{store: store, prefix: prefix, partitions: partitions}
}

// AssignPrefix is the key prefix every assignment lives under.
func (a *Assigner) AssignPrefix() string {
	return a.prefix + "assign/"
}

func (a *Assigner) key(partition int) string {
	return a.AssignPrefix() + strconv.Itoa(partition)
}

// List returns the current ownership map keyed by partition.
func (a *Assigner) List(ctx context.Context) (map[int]Assignment, error) {
	kvs, err := a.store.List(ctx, a.AssignPrefix())
	if err != nil {
		return nil, err
	}
	out := make(map[int]Assignment, len(kvs))
	for _, kv := range kvs {
		partition, err := strconv.Atoi(kv.Key[len(a.AssignPrefix()):])
		if err != nil {
			log.Error().Str("key", kv.Key).Msg("cluster: bad assignment key")
			continue
		}
		var value assignmentValue
		if err := json.Unmarshal(kv.Value, &value); err != nil {
			log.Error().Err(err).Str("key", kv.Key).Msg("cluster: bad assignment value")
			continue
		}
		out[partition] = Assignment{Partition: partition, Owner: value.Owner, Epoch: kv.Revision}
	}
	return out, nil
}

// Set installs owner for a partition. rev is the revision the caller last saw
// for that partition (0 when it was unassigned); the write is rejected when
// somebody else changed it meanwhile, which keeps epochs strictly ordered.
func (a *Assigner) Set(ctx context.Context, partition int, owner string, rev int64) (Assignment, bool, error) {
	value, err := json.Marshal(assignmentValue{Owner: owner})
	if err != nil {
		return Assignment{}, false, err
	}
	newRev, ok, err := a.store.CompareAndSwap(ctx, a.key(partition), value, rev, NoLease)
	if err != nil || !ok {
		return Assignment{}, false, err
	}
	return Assignment{Partition: partition, Owner: owner, Epoch: newRev}, true, nil
}

// Apply writes a rebalance result. Partitions whose assignment changed under
// us are skipped, the next rebalance round picks them up again.
func (a *Assigner) Apply(ctx context.Context, current map[int]Assignment, changes map[int]string) error {
	partitions := make([]int, 0, len(changes))
	for partition := range changes {
		partitions = append(partitions, partition)
	}
	sort.Ints(partitions)

	for _, partition := range partitions {
		owner := changes[partition]
		_, ok, err := a.Set(ctx, partition, owner, current[partition].Epoch)
		if err != nil {
			return err
		}
		if !ok {
			log.Warn().Int("partition", partition).Msg("cluster: assignment changed concurrently, skipped")
			continue
		}
		log.Info().Int("partition", partition).Str("owner", owner).Msg("cluster: partition assigned")
	}
	return nil
}

// Rebalance spreads `partitions` buckets over `nodes` and returns only the
// partitions that must move.
//
// Stability is the point: a partition already owned by a live node is left
// alone unless that node is over quota. Adding or removing a node therefore
// moves roughly partitions/nodes buckets instead of reshuffling everything.
func Rebalance(current map[int]string, nodes []string, partitions int) map[int]string {
	if len(nodes) == 0 || partitions <= 0 {
		return nil
	}
	nodes = append([]string(nil), nodes...)
	sort.Strings(nodes)

	alive := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		alive[node] = true
	}

	owned := make(map[string][]int, len(nodes))
	pool := make([]int, 0, partitions)
	for partition := 0; partition < partitions; partition++ {
		owner, ok := current[partition]
		if ok && alive[owner] {
			owned[owner] = append(owned[owner], partition)
			continue
		}
		pool = append(pool, partition)
	}

	average := partitions / len(nodes)
	remainder := partitions % len(nodes)
	quota := func(i int) int {
		if i < remainder {
			return average + 1
		}
		return average
	}

	// give back everything above quota, lowest partitions are kept
	for i, node := range nodes {
		list := owned[node]
		sort.Ints(list)
		if len(list) > quota(i) {
			pool = append(pool, list[quota(i):]...)
			owned[node] = list[:quota(i)]
			continue
		}
		owned[node] = list
	}
	sort.Ints(pool)

	changes := make(map[int]string)
	for i, node := range nodes {
		need := quota(i) - len(owned[node])
		for ; need > 0 && len(pool) > 0; need-- {
			partition := pool[0]
			pool = pool[1:]
			changes[partition] = node
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return changes
}
