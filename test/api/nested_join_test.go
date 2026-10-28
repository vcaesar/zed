/* Copyright 2026 AtomAI
*
* Licensed under Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with License.
* You may obtain copy of License at
*
* http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under License is distributed on "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See License for specific language governing permissions and
* limitations under License.
 */

package api

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

const nestedIndexName = "TestNestedJoin.index_1"

// searchNestedIndex runs ES search over nested index and returns hit ids.
func searchNestedIndex(t *testing.T, query string) ([]string, int) {
	t.Helper()

	body := bytes.NewBufferString(query)
	resp := request("POST", "/es/"+nestedIndexName+"/_search", body)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	data := new(meta.SearchResponse)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), data))

	ids := make([]string, 0, len(data.Hits.Hits))
	for _, hit := range data.Hits.Hits {
		ids = append(ids, hit.ID)
	}

	return ids, data.Hits.Total.Value
}

func TestNestedJoinSearch(t *testing.T) {
	t.Run("create index with nested and join mappings", func(t *testing.T) {
		request("DELETE", "/api/index/"+nestedIndexName, nil) // drop leftovers from previous run

		body := bytes.NewBufferString(`{
			"name": "` + nestedIndexName + `",
			"storage_type": "disk",
			"mappings": {
				"properties": {
					"title": {"type": "text"},
					"comments": {
						"type": "nested",
						"properties": {
							"name": {"type": "keyword"},
							"stars": {"type": "numeric"}
						}
					},
					"my_join": {"type": "join", "relations": {"question": ["answer"]}}
				}
			}
		}`)
		resp := request("PUT", "/api/index", body)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	})

	t.Run("index documents", func(t *testing.T) {
		docs := map[string]string{
			"q1": `{"title": "question one", "my_join": "question", "comments": [{"name": "alice", "stars": 5}, {"name": "bob", "stars": 1}]}`,
			"q2": `{"title": "question two", "my_join": "question", "comments": [{"name": "bob", "stars": 5}]}`,
			"a1": `{"title": "answer one", "my_join": {"name": "answer", "parent": "q1"}}`,
		}
		for docID, doc := range docs {
			resp := request("PUT", "/api/"+nestedIndexName+"/_doc/"+docID, bytes.NewBufferString(doc))
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		}
		for docID := range docs {
			waitForDocument(t, nestedIndexName, docID)
		}
	})

	t.Run("POST /es/:target/_search", func(t *testing.T) {
		tests := []struct {
			name  string
			query string
			want  []string
		}{
			{
				name:  "match_all hides nested documents",
				query: `{"query": {"match_all": {}}, "size": 20}`,
				want:  []string{"q1", "q2", "a1"},
			},
			{
				name:  "nested field is not searchable on root",
				query: `{"query": {"term": {"comments.name": "alice"}}, "size": 20}`,
				want:  []string{},
			},
			{
				name:  "nested query",
				query: `{"query": {"nested": {"path": "comments", "query": {"term": {"comments.name": "bob"}}}}, "size": 20}`,
				want:  []string{"q1", "q2"},
			},
			{
				name: "nested query on same element",
				query: `{"query": {"nested": {"path": "comments", "score_mode": "avg", "query": {"bool": {"must": [
					{"term": {"comments.name": "bob"}},
					{"term": {"comments.stars": 5}}
				]}}}}, "size": 20}`,
				want: []string{"q2"},
			},
			{
				name:  "has_child",
				query: `{"query": {"has_child": {"type": "answer", "query": {"match": {"title": "one"}}}}, "size": 20}`,
				want:  []string{"q1"},
			},
			{
				name:  "has_parent",
				query: `{"query": {"has_parent": {"parent_type": "question", "query": {"match": {"title": "one"}}}}, "size": 20}`,
				want:  []string{"a1"},
			},
			{
				name:  "parent_id",
				query: `{"query": {"parent_id": {"type": "answer", "id": "q1"}}, "size": 20}`,
				want:  []string{"a1"},
			},
			{
				name: "function_score",
				query: `{"query": {"function_score": {"query": {"match_all": {}}, "weight": 3,
					"boost_mode": "replace"}}, "size": 20}`,
				want: []string{"q1", "q2", "a1"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ids, total := searchNestedIndex(t, tt.query)
				assert.ElementsMatch(t, tt.want, ids)
				assert.Equal(t, len(tt.want), total)
			})
		}
	})

	t.Run("function_score scores documents", func(t *testing.T) {
		body := bytes.NewBufferString(`{"query": {"function_score": {
			"query": {"match_all": {}},
			"field_value_factor": {"field": "_nested_count.comments", "missing": 1},
			"boost_mode": "replace"
		}}, "size": 20}`)
		resp := request("POST", "/es/"+nestedIndexName+"/_search", body)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

		data := new(meta.SearchResponse)
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), data))
		scores := make(map[string]float64, len(data.Hits.Hits))
		for _, hit := range data.Hits.Hits {
			scores[hit.ID] = hit.Score
		}
		assert.Equal(t, 2.0, scores["q1"])
		assert.Equal(t, 1.0, scores["q2"])
		assert.Equal(t, 1.0, scores["a1"])
	})

	t.Run("delete document removes nested documents", func(t *testing.T) {
		resp := request("DELETE", "/api/"+nestedIndexName+"/_doc/q1", nil)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

		assert.Eventually(t, func() bool {
			return request("GET", "/api/"+nestedIndexName+"/_doc/q1%23comments%230", nil).Code == http.StatusBadRequest
		}, 30*time.Second, 50*time.Millisecond, "hidden documents of deleted root must be removed")

		ids, _ := searchNestedIndex(t, `{"query": {"nested": {"path": "comments", "query": {"term": {"comments.name": "alice"}}}}, "size": 20}`)
		assert.Empty(t, ids)
	})

	t.Run("cleanup", func(t *testing.T) {
		resp := request("DELETE", "/api/index/"+nestedIndexName, nil)
		assert.Equal(t, http.StatusOK, resp.Code)
	})
}
