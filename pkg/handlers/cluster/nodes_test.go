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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	zcluster "github.com/zincsearch/zincsearch/pkg/cluster"
	"github.com/zincsearch/zincsearch/pkg/config"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
	"github.com/zincsearch/zincsearch/test/utils"
)

func TestNodesSingleNode(t *testing.T) {
	require.False(t, zcluster.Enabled(), "the test binary runs without clustering")

	c, w := utils.NewGinContext()
	Nodes(c)
	require.Equal(t, http.StatusOK, w.Code)

	resp := new(NodesResponse)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), resp))
	assert.False(t, resp.Enabled)
	require.Len(t, resp.Nodes, 1, "a process without clustering is a cluster of one")
	assert.True(t, resp.Nodes[0].Alive)
	assert.True(t, resp.Nodes[0].IsLeader)
	assert.NotEmpty(t, resp.Nodes[0].ID)
}

func TestPartitions(t *testing.T) {
	previous := config.Global.Cluster.Partitions
	config.Global.Cluster.Partitions = 16
	t.Cleanup(func() { config.Global.Cluster.Partitions = previous })

	t.Run("single index", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestURL(c, "/api/cluster/partitions", map[string]string{"index": "logs-2024"})
		Partitions(c)
		require.Equal(t, http.StatusOK, w.Code)

		resp := new(PartitionsResponse)
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), resp))
		assert.False(t, resp.Enabled)
		assert.Equal(t, 16, resp.Partitions)
		require.Len(t, resp.Owners, 1)
		assert.Equal(t, "logs-2024", resp.Owners[0].Index)
		assert.Equal(t, zcluster.PartitionOf("logs-2024"), resp.Owners[0].Partition)
		assert.True(t, resp.Owners[0].Local, "without an ownership map every index is served locally")
		assert.Empty(t, resp.Owners[0].Owner)
	})

	t.Run("whole map", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestURL(c, "/api/cluster/partitions", nil)
		Partitions(c)
		require.Equal(t, http.StatusOK, w.Code)

		resp := new(PartitionsResponse)
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), resp))
		assert.Empty(t, resp.Owners, "nothing is assigned while clustering is off")
	})
}
