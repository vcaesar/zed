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
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/vcaesar/zedsearch/pkg/cluster"
	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta/elastic"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

// Health returns the health of the cluster
//
// ZedSearch runs on a single node, there is no shard allocation, so the
// cluster is always green with one node and no relocating/unassigned shards.
//
// @Id ClusterHealth
// @Summary Get cluster health for compatible ES
// @security BasicAuth
// @Tags    Cluster
// @Produce json
// @Param   index  path  string  false  "Index"
// @Success 200 {object} elastic.ClusterHealthResponse
// @Router /es/_cluster/health [get]
func Health(c *gin.Context) {
	shards := activeShards(c.Param("target"))
	nodes := len(cluster.Nodes())
	zutils.GinRenderJSON(c, http.StatusOK, elastic.ClusterHealthResponse{
		ClusterName:                 clusterName(),
		Status:                      statusGreen,
		TimedOut:                    false,
		NumberOfNodes:               nodes,
		NumberOfDataNodes:           nodes,
		ActivePrimaryShards:         shards,
		ActiveShards:                shards,
		RelocatingShards:            0,
		InitializingShards:          0,
		UnassignedShards:            0,
		DelayedUnassignedShards:     0,
		NumberOfPendingTasks:        0,
		NumberOfInFlightFetch:       0,
		TaskMaxWaitingInQueueMillis: 0,
		ActiveShardsPercentAsNumber: 100.0,
	})
}

// activeShards sums the shards of every index matching the target patterns.
func activeShards(target string) int64 {
	patterns := elastic.SplitPatterns(target)
	var shards int64
	for _, index := range core.ZINC_INDEX_LIST.List() {
		if !elastic.MatchAnyPattern(index.GetName(), patterns) {
			continue
		}
		shards += index.GetShardNum()
	}
	return shards
}
