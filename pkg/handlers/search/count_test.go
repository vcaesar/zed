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

package search

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/test/utils"
)

func TestCount(t *testing.T) {
	indexName := "TestCount.index_1"
	type args struct {
		code   int
		data   string
		params map[string]string
		result string
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "count all with empty body",
			args: args{
				code:   http.StatusOK,
				data:   "",
				params: map[string]string{"target": indexName},
				result: `{"count":3,"_shards":{"total":2,"successful":2,"skipped":0,"failed":0}}`,
			},
		},
		{
			name: "count with query",
			args: args{
				code:   http.StatusOK,
				data:   `{"query":{"match":{"name":"zed"}}}`,
				params: map[string]string{"target": indexName},
				result: `{"count":2,`,
			},
		},
		{
			name: "count ignores size",
			args: args{
				code:   http.StatusOK,
				data:   `{"query":{"match_all":{}},"size":1}`,
				params: map[string]string{"target": indexName},
				result: `{"count":3,`,
			},
		},
		{
			name: "query json error",
			args: args{
				code:   http.StatusBadRequest,
				data:   `{"query":{"match_all":{x}}}`,
				params: map[string]string{"target": indexName},
				result: "invalid character",
			},
		},
		{
			name: "index not found",
			args: args{
				code:   http.StatusBadRequest,
				data:   "",
				params: map[string]string{"target": "NotExist" + indexName},
				result: "does not exists",
			},
		},
	}

	t.Run("prepare", func(t *testing.T) {
		index, err := core.NewIndex(indexName, "disk", 2)
		assert.NoError(t, err)
		assert.NoError(t, core.StoreIndex(index))
		assert.NoError(t, index.CreateDocument("1", map[string]interface{}{"name": "zed", "num": 1}, false))
		assert.NoError(t, index.CreateDocument("2", map[string]interface{}{"name": "zed", "num": 2}, false))
		assert.NoError(t, index.CreateDocument("3", map[string]interface{}{"name": "other", "num": 3}, false))
		assert.Eventually(t, func() bool {
			res, err := index.Search(&meta.ZedQuery{Size: 10})
			return err == nil && res.Hits.Total.Value == 3
		}, 10*time.Second, 100*time.Millisecond, "documents not indexed")
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := utils.NewContext()
			utils.SetRequestData(c, tt.args.data)
			utils.SetRequestParams(c, tt.args.params)
			Count(c)
			assert.Equal(t, tt.args.code, w.Code)
			assert.Contains(t, w.Body.String(), tt.args.result)
		})
	}

	t.Run("count with nil body", func(t *testing.T) {
		c, w := utils.NewContext()
		utils.SetRequestParams(c, map[string]string{"target": indexName})
		Count(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `{"count":3,`)
	})

	t.Run("cleanup", func(t *testing.T) {
		assert.NoError(t, core.DeleteIndex(indexName))
	})
}

func TestCountShards(t *testing.T) {
	indexName := "TestCountShards.index_1"

	t.Run("prepare", func(t *testing.T) {
		index, err := core.NewIndex(indexName, "disk", 3)
		assert.NoError(t, err)
		assert.NoError(t, core.StoreIndex(index))
	})

	tests := []struct {
		name       string
		indexNames []string
		want       int64
	}{
		{name: "exact index", indexNames: []string{indexName}, want: 3},
		{name: "glob index", indexNames: []string{"TestCountShards.*"}, want: 3},
		{name: "unknown index falls back to 1", indexNames: []string{"NotExist"}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, countShards(tt.indexNames))
		})
	}

	t.Run("cleanup", func(t *testing.T) {
		assert.NoError(t, core.DeleteIndex(indexName))
	})
}
