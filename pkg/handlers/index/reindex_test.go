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

package index

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/meta/elastic"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
	"github.com/vcaesar/zedsearch/test/utils"
)

func TestReindex(t *testing.T) {
	sourceName := "TestReindex.index_1"
	destName := "TestReindex.index_2"
	limitedName := "TestReindex.index_3"

	t.Run("prepare", func(t *testing.T) {
		index, err := core.NewIndex(sourceName, "disk", 2)
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

	t.Run("reindex all documents", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, `{"source":{"index":"`+sourceName+`"},"dest":{"index":"`+destName+`"}}`)
		Reindex(c)
		assert.Equal(t, http.StatusOK, w.Code)

		resp := new(elastic.ReindexResponse)
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), resp))
		assert.Equal(t, 3, resp.Total)
		assert.Equal(t, 3, resp.Created)
		assert.Equal(t, 1, resp.Batches)
		assert.Empty(t, resp.Failures)
		assert.Equal(t, -1, resp.RequestsPerSecond)

		dest, exists := core.GetIndex(destName)
		assert.True(t, exists)
		assert.Eventually(t, func() bool {
			res, err := dest.Search(&meta.ZedQuery{Size: 10, Sort: []interface{}{"_id"}})
			return err == nil && res.Hits.Total.Value == 3
		}, 10*time.Second, 100*time.Millisecond)

		// _id and _source are preserved
		res, err := dest.Search(&meta.ZedQuery{Size: 10, Sort: []interface{}{"_id"}})
		assert.NoError(t, err)
		ids := make([]string, 0, len(res.Hits.Hits))
		for _, hit := range res.Hits.Hits {
			ids = append(ids, hit.ID)
		}
		assert.ElementsMatch(t, []string{"1", "2", "3"}, ids)
		source := res.Hits.Hits[0].Source.(map[string]interface{})
		assert.Equal(t, "zed", source["name"])
	})

	t.Run("reindex with query and max_docs", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, `{"source":{"index":"`+sourceName+`","query":{"match":{"name":"zed"}}},"dest":{"index":"`+limitedName+`"},"max_docs":1}`)
		Reindex(c)
		assert.Equal(t, http.StatusOK, w.Code)

		resp := new(elastic.ReindexResponse)
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), resp))
		assert.Equal(t, 1, resp.Total)
		assert.Equal(t, 1, resp.Created)
	})

	type args struct {
		code   int
		data   string
		result string
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "body json error",
			args: args{
				code:   http.StatusBadRequest,
				data:   `{"source":{x}}`,
				result: "invalid character",
			},
		},
		{
			name: "missing dest index",
			args: args{
				code:   http.StatusBadRequest,
				data:   `{"source":{"index":"` + sourceName + `"}}`,
				result: "source.index and dest.index are required",
			},
		},
		{
			name: "same source and dest",
			args: args{
				code:   http.StatusBadRequest,
				data:   `{"source":{"index":"` + sourceName + `"},"dest":{"index":"` + sourceName + `"}}`,
				result: "must be different",
			},
		},
		{
			name: "source index not found",
			args: args{
				code:   http.StatusBadRequest,
				data:   `{"source":{"index":"NotExist` + sourceName + `"},"dest":{"index":"` + destName + `"}}`,
				result: "does not exists",
			},
		},
		{
			name: "invalid dest index name",
			args: args{
				code:   http.StatusBadRequest,
				data:   `{"source":{"index":"` + sourceName + `"},"dest":{"index":"_invalid"}}`,
				result: "index name cannot start with _",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := utils.NewGinContext()
			utils.SetGinRequestData(c, tt.args.data)
			Reindex(c)
			assert.Equal(t, tt.args.code, w.Code)
			assert.Contains(t, w.Body.String(), tt.args.result)
		})
	}

	t.Run("nil body", func(t *testing.T) {
		c, w := utils.NewGinContext()
		Reindex(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "source.index and dest.index are required")
	})

	t.Run("cleanup", func(t *testing.T) {
		assert.NoError(t, core.DeleteIndex(sourceName))
		assert.NoError(t, core.DeleteIndex(destName))
		assert.NoError(t, core.DeleteIndex(limitedName))
	})
}
