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
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

func TestSearchVector(t *testing.T) {
	index := "TestSearchVector.index_1"
	request("DELETE", "/api/index/"+index, nil) // drop leftovers from previous run

	t.Run("reject bad vector mapping", func(t *testing.T) {
		resp := request("PUT", "/api/index", bytes.NewBufferString(`{
			"name": "`+index+`",
			"mappings": {"properties": {"embedding": {"type": "vector", "dims": -1}}}
		}`))
		assert.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
		assert.Contains(t, resp.Body.String(), "dims")
	})

	t.Run("create index with vector mapping", func(t *testing.T) {
		resp := request("PUT", "/api/index", bytes.NewBufferString(`{
			"name": "`+index+`",
			"storage_type": "disk",
			"mappings": {"properties": {
				"name": {"type": "keyword"},
				"embedding": {"type": "dense_vector", "dims": 3}
			}}
		}`))
		assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

		resp = request("GET", "/api/"+index+"/_mapping", nil)
		assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		assert.Contains(t, resp.Body.String(), `"type":"vector"`)
		assert.Contains(t, resp.Body.String(), `"dims":3`)
	})

	docs := map[string]string{
		"x": `{"name": "x", "embedding": [1, 0, 0]}`,
		"y": `{"name": "y", "embedding": [0, 1, 0]}`,
		"z": `{"name": "z", "embedding": [0, 0, 1]}`,
		"xy": `{"name": "xy", "embedding": [1, 1, 0]}`,
	}
	t.Run("index documents", func(t *testing.T) {
		for id, doc := range docs {
			resp := request("PUT", "/api/"+index+"/_doc/"+id, bytes.NewBufferString(doc))
			assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		}
		for id := range docs {
			waitForDocument(t, index, id)
		}
	})

	t.Run("reject invalid vectors", func(t *testing.T) {
		for name, doc := range map[string]string{
			"wrong dims":  `{"embedding": [1, 0]}`,
			"not numeric": `{"embedding": ["a", "b", "c"]}`,
			"scalar":      `{"embedding": 1}`,
		} {
			resp := request("PUT", "/api/"+index+"/_doc/bad", bytes.NewBufferString(doc))
			// document validation errors are reported as 500 by handler, same as bad geo_point
			assert.Equal(t, http.StatusInternalServerError, resp.Code, name)
			assert.Contains(t, resp.Body.String(), "vector", name)
		}
	})

	search := func(t *testing.T, path, query string) *meta.SearchResponse {
		t.Helper()
		resp := request("POST", path, bytes.NewBufferString(query))
		assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		data := new(meta.SearchResponse)
		assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), data))
		return data
	}
	ids := func(data *meta.SearchResponse) []string {
		out := make([]string, 0, len(data.Hits.Hits))
		for _, h := range data.Hits.Hits {
			out = append(out, h.ID)
		}
		return out
	}

	t.Run("knn cosine ordered by similarity", func(t *testing.T) {
		// k is per shard; size caps the merged result like Elasticsearch
		data := search(t, "/es/"+index+"/_search", `{"size": 2, "query": {"knn": {"field": "embedding", "query_vector": [1, 0.1, 0], "k": 2}}}`)
		assert.Equal(t, []string{"x", "xy"}, ids(data))
		assert.Greater(t, data.Hits.Hits[0].Score, data.Hits.Hits[1].Score)
	})

	t.Run("knn l2 via opensearch form", func(t *testing.T) {
		data := search(t, "/es/"+index+"/_search", `{"size": 1, "query": {"knn": {"embedding": {"vector": [0, 0, 1], "k": 1, "space_type": "l2"}}}}`)
		assert.Equal(t, []string{"z"}, ids(data))
	})

	t.Run("knn composes with bool filter", func(t *testing.T) {
		data := search(t, "/es/"+index+"/_search", `{"query": {"bool": {
			"must": {"knn": {"field": "embedding", "query_vector": [1, 1, 0], "k": 4}},
			"filter": {"term": {"name": "y"}}}}}`)
		assert.Equal(t, []string{"y"}, ids(data))
	})

	t.Run("knn rejects dims mismatch", func(t *testing.T) {
		resp := request("POST", "/es/"+index+"/_search", bytes.NewBufferString(
			`{"query": {"knn": {"field": "embedding", "query_vector": [1, 0]}}}`))
		assert.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
		assert.Contains(t, resp.Body.String(), "dimensions")
	})

	t.Run("knn on non vector field", func(t *testing.T) {
		resp := request("POST", "/es/"+index+"/_search", bytes.NewBufferString(
			`{"query": {"knn": {"field": "name", "query_vector": [1, 0, 0]}}}`))
		assert.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
	})

	t.Run("cleanup", func(t *testing.T) {
		resp := request("DELETE", "/api/index/"+index, nil)
		assert.Equal(t, http.StatusOK, resp.Code)
	})
}
