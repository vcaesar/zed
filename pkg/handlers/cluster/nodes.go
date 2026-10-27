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
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"

	"github.com/zincsearch/zincsearch/pkg/cluster"
	"github.com/zincsearch/zincsearch/pkg/config"
	"github.com/zincsearch/zincsearch/pkg/zutils"
)

// NodesResponse is the payload of GET /api/cluster/nodes.
type NodesResponse struct {
	ClusterName string               `json:"cluster_name"`
	Enabled     bool                 `json:"enabled"`
	Nodes       []cluster.NodeStatus `json:"nodes"`
}

// PartitionOwner describes who owns one partition.
type PartitionOwner struct {
	Partition int    `json:"partition"`
	Owner     string `json:"owner"`
	Epoch     int64  `json:"epoch"`
	Local     bool   `json:"local"`
	Index     string `json:"index,omitempty"`
}

// PartitionsResponse is the payload of GET /api/cluster/partitions.
type PartitionsResponse struct {
	Enabled    bool             `json:"enabled"`
	Partitions int              `json:"partitions"`
	Owners     []PartitionOwner `json:"owners"`
}

// Nodes lists the members of the cluster
//
// With clustering disabled the local process is reported as the single
// node, so dashboards do not need a special case.
//
// @Id ClusterNodes
// @Summary List cluster nodes
// @security BasicAuth
// @Tags Cluster
// @Produce json
// @Success 200 {object} NodesResponse
// @Router /api/cluster/nodes [get]
func Nodes(c *gin.Context) {
	zutils.GinRenderJSON(c, http.StatusOK, NodesResponse{
		ClusterName: config.Global.Cluster.Name,
		Enabled:     cluster.Enabled(),
		Nodes:       cluster.Nodes(),
	})
}

// Partitions reports the index ownership map
//
// With `index` set only the partition of that index is returned, which is
// the quickest way to find out which node serves it.
//
// @Id ClusterPartitions
// @Summary Get partition ownership
// @security BasicAuth
// @Tags Cluster
// @Produce json
// @Param index query string false "Index"
// @Success 200 {object} PartitionsResponse
// @Router /api/cluster/partitions [get]
func Partitions(c *gin.Context) {
	local := ""
	if node := cluster.Local(); node != nil {
		local = node.ID
	}

	if indexName := c.Query("index"); indexName != "" {
		owner, epoch, ok := cluster.Owner(indexName)
		partition := PartitionOwner{
			Partition: cluster.PartitionOf(indexName),
			Index:     indexName,
			Local:     cluster.IsLocalOwner(indexName),
		}
		if ok {
			partition.Owner = owner
			partition.Epoch = epoch
		}
		zutils.GinRenderJSON(c, http.StatusOK, PartitionsResponse{
			Enabled:    cluster.Enabled(),
			Partitions: config.Global.Cluster.Partitions,
			Owners:     []PartitionOwner{partition},
		})
		return
	}

	assignments := cluster.Assignments()
	owners := make([]PartitionOwner, 0, len(assignments))
	for _, assignment := range assignments {
		owners = append(owners, PartitionOwner{
			Partition: assignment.Partition,
			Owner:     assignment.Owner,
			Epoch:     assignment.Epoch,
			Local:     assignment.Owner == local,
		})
	}
	sort.Slice(owners, func(i, j int) bool { return owners[i].Partition < owners[j].Partition })

	zutils.GinRenderJSON(c, http.StatusOK, PartitionsResponse{
		Enabled:    cluster.Enabled(),
		Partitions: config.Global.Cluster.Partitions,
		Owners:     owners,
	})
}
