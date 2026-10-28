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

package api

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/meta/elastic"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

func TestApiESCompat(t *testing.T) {
	esIndex := "escompat"
	esIndexCopy := "escompatcopy"
	esBulkData := `{"index": {"_index": "escompat", "_id": "1"}}
{"name": "zinc", "num": 1}
{"index": {"_index": "escompat", "_id": "2"}}
{"name": "zinc", "num": 2}
{"index": {"_index": "escompat", "_id": "3"}}
{"name": "other", "num": 3}`

	t.Run("prepare", func(t *testing.T) {
		body := bytes.NewBufferString(`{"name": "` + esIndex + `", "storage_type": "disk"}`)
		resp := request("PUT", "/api/index", body)
		assert.Equal(t, http.StatusOK, resp.Code)

		resp = request("POST", "/es/"+esIndex+"/_bulk", bytes.NewBufferString(esBulkData))
		assert.Equal(t, http.StatusOK, resp.Code)
		// docs land in different shards, each shard refreshes independently
		for _, id := range []string{"1", "2", "3"} {
			waitForDocument(t, esIndex, id)
		}
	})

	t.Run("GET /es/:target/_count", func(t *testing.T) {
		resp := request("GET", "/es/"+esIndex+"/_count", nil)
		assert.Equal(t, http.StatusOK, resp.Code)

		data := new(elastic.CountResponse)
		assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), data))
		assert.Equal(t, int64(3), data.Count)
		assert.Equal(t, data.Shards.Total, data.Shards.Successful)
		assert.Equal(t, int64(0), data.Shards.Failed)
	})

	t.Run("POST /es/:target/_count with query", func(t *testing.T) {
		body := bytes.NewBufferString(`{"query":{"match":{"name":"zinc"}}}`)
		resp := request("POST", "/es/"+esIndex+"/_count", body)
		assert.Equal(t, http.StatusOK, resp.Code)

		data := new(elastic.CountResponse)
		assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), data))
		assert.Equal(t, int64(2), data.Count)
	})

	t.Run("GET /api/:target/_count", func(t *testing.T) {
		resp := request("GET", "/api/"+esIndex+"/_count", nil)
		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Contains(t, resp.Body.String(), `"count":3`)
	})

	t.Run("GET /es/_cat/indices", func(t *testing.T) {
		resp := request("GET", "/es/_cat/indices?format=json", nil)
		assert.Equal(t, http.StatusOK, resp.Code)

		items := make([]elastic.CatIndexResponse, 0)
		assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), &items))
		found := false
		for _, item := range items {
			if item.Index == esIndex {
				found = true
				assert.Equal(t, "green", item.Health)
				assert.Equal(t, "open", item.Status)
			}
		}
		assert.True(t, found, "index %s not listed", esIndex)

		resp = request("GET", "/es/_cat/indices/"+esIndex+"?v", nil)
		assert.Equal(t, http.StatusOK, resp.Code)
		lines := strings.Split(strings.TrimRight(resp.Body.String(), "\n"), "\n")
		assert.Len(t, lines, 2)
		assert.True(t, strings.HasPrefix(lines[0], "health status index"), lines[0])
		assert.Contains(t, lines[1], esIndex)
	})

	t.Run("GET /es/_cat/health", func(t *testing.T) {
		resp := request("GET", "/es/_cat/health?format=json", nil)
		assert.Equal(t, http.StatusOK, resp.Code)

		items := make([]elastic.CatHealthResponse, 0)
		assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), &items))
		assert.Len(t, items, 1)
		assert.Equal(t, "zedsearch", items[0].Cluster)
		assert.Equal(t, "green", items[0].Status)

		resp = request("GET", "/es/_cat/health", nil)
		assert.Equal(t, http.StatusOK, resp.Code)
		assert.NotContains(t, resp.Body.String(), "epoch")
	})

	t.Run("GET /es/_cluster/health", func(t *testing.T) {
		resp := request("GET", "/es/_cluster/health", nil)
		assert.Equal(t, http.StatusOK, resp.Code)

		data := new(elastic.ClusterHealthResponse)
		assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), data))
		assert.Equal(t, "zedsearch", data.ClusterName)
		assert.Equal(t, "green", data.Status)
		assert.Equal(t, 1, data.NumberOfNodes)
		assert.Equal(t, 100.0, data.ActiveShardsPercentAsNumber)
		assert.Greater(t, data.ActiveShards, int64(0))

		resp = request("GET", "/es/_cluster/health/"+esIndex, nil)
		assert.Equal(t, http.StatusOK, resp.Code)

		data = new(elastic.ClusterHealthResponse)
		assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), data))
		assert.Greater(t, data.ActivePrimaryShards, int64(0))
	})

	t.Run("GET /es/:target/_field_caps", func(t *testing.T) {
		resp := request("GET", "/es/"+esIndex+"/_field_caps?fields=*", nil)
		assert.Equal(t, http.StatusOK, resp.Code)

		data := new(elastic.FieldCapsResponse)
		assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), data))
		assert.Contains(t, data.Indices, esIndex)
		assert.Equal(t, elastic.FieldCap{
			Type:          "double",
			MetadataField: false,
			Searchable:    true,
			Aggregatable:  true,
		}, data.Fields["num"]["double"])
		assert.Contains(t, data.Fields, "name")

		resp = request("GET", "/es/"+esIndex+"/_field_caps?fields=na*", nil)
		assert.Equal(t, http.StatusOK, resp.Code)

		data = new(elastic.FieldCapsResponse)
		assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), data))
		assert.Contains(t, data.Fields, "name")
		assert.NotContains(t, data.Fields, "num")
	})

	t.Run("POST /es/_reindex", func(t *testing.T) {
		body := bytes.NewBufferString(`{"source":{"index":"` + esIndex + `"},"dest":{"index":"` + esIndexCopy + `"}}`)
		resp := request("POST", "/es/_reindex", body)
		assert.Equal(t, http.StatusOK, resp.Code)

		data := new(elastic.ReindexResponse)
		assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), data))
		assert.Equal(t, 3, data.Total)
		assert.Equal(t, 3, data.Created)
		assert.Empty(t, data.Failures)

		require.Eventually(t, func() bool {
			resp := request("GET", "/es/"+esIndexCopy+"/_count", nil)
			return resp.Code == http.StatusOK && strings.Contains(resp.Body.String(), `"count":3`)
		}, 10*time.Second, 100*time.Millisecond, "documents not reindexed")
	})

	t.Run("POST /api/_reindex with same source and dest", func(t *testing.T) {
		body := bytes.NewBufferString(`{"source":{"index":"` + esIndex + `"},"dest":{"index":"` + esIndex + `"}}`)
		resp := request("POST", "/api/_reindex", body)
		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "must be different")
	})

	t.Run("cleanup", func(t *testing.T) {
		resp := request("DELETE", "/api/index/"+esIndex, nil)
		assert.Equal(t, http.StatusOK, resp.Code)
		resp = request("DELETE", "/api/index/"+esIndexCopy, nil)
		assert.Equal(t, http.StatusOK, resp.Code)
	})
}
