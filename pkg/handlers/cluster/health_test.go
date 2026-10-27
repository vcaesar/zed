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

	"github.com/zincsearch/zincsearch/pkg/core"
	"github.com/zincsearch/zincsearch/pkg/meta/elastic"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
	"github.com/zincsearch/zincsearch/test/utils"
)

func TestHealth(t *testing.T) {
	indexName := "TestHealth.index_1"

	t.Run("prepare", func(t *testing.T) {
		index, err := core.NewIndex(indexName, "disk", 3)
		assert.NoError(t, err)
		assert.NoError(t, core.StoreIndex(index))
	})

	type args struct {
		params     map[string]string
		wantShards int64
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "single index",
			args: args{params: map[string]string{"target": indexName}, wantShards: 3},
		},
		{
			name: "index glob",
			args: args{params: map[string]string{"target": "TestHealth.*"}, wantShards: 3},
		},
		{
			name: "unknown index",
			args: args{params: map[string]string{"target": "NotExist" + indexName}, wantShards: 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := utils.NewGinContext()
			utils.SetGinRequestParams(c, tt.args.params)
			Health(c)
			assert.Equal(t, http.StatusOK, w.Code)

			resp := new(elastic.ClusterHealthResponse)
			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), resp))
			assert.Equal(t, ClusterName, resp.ClusterName)
			assert.Equal(t, "green", resp.Status)
			assert.False(t, resp.TimedOut)
			assert.Equal(t, 1, resp.NumberOfNodes)
			assert.Equal(t, 1, resp.NumberOfDataNodes)
			assert.Equal(t, tt.args.wantShards, resp.ActivePrimaryShards)
			assert.Equal(t, tt.args.wantShards, resp.ActiveShards)
			assert.Equal(t, 0, resp.UnassignedShards)
			assert.Equal(t, 100.0, resp.ActiveShardsPercentAsNumber)
		})
	}

	t.Run("all indexes", func(t *testing.T) {
		c, w := utils.NewGinContext()
		Health(c)
		assert.Equal(t, http.StatusOK, w.Code)

		resp := new(elastic.ClusterHealthResponse)
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), resp))
		assert.GreaterOrEqual(t, resp.ActiveShards, int64(3))
	})

	t.Run("cleanup", func(t *testing.T) {
		assert.NoError(t, core.DeleteIndex(indexName))
	})
}
