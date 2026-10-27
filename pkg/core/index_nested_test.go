/* Copyright 2022 Zinc Labs Inc. and Contributors
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

package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch/pkg/errors"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/uquery/mappings"
)

// nestedIndexMappings maps nested comments and question/answer join.
func nestedIndexMappings(t *testing.T) *meta.Mappings {
	t.Helper()

	m, err := mappings.Request(nil, map[string]interface{}{
		"properties": map[string]interface{}{
			"title": map[string]interface{}{"type": "text"},
			"comments": map[string]interface{}{
				"type": "nested",
				"properties": map[string]interface{}{
					"name":  map[string]interface{}{"type": "keyword"},
					"stars": map[string]interface{}{"type": "numeric"},
				},
			},
			"my_join": map[string]interface{}{
				"type":      "join",
				"relations": map[string]interface{}{"question": []interface{}{"answer"}},
			},
		},
	})
	require.NoError(t, err)

	return m
}

// searchIDs runs query and returns ids of hits and reported total.
func searchIDs(t *testing.T, index *Index, query interface{}) ([]string, int) {
	t.Helper()

	resp, err := index.Search(&meta.ZincQuery{Query: query, Size: 20})
	require.NoError(t, err)

	ids := make([]string, 0, len(resp.Hits.Hits))
	for _, hit := range resp.Hits.Hits {
		ids = append(ids, hit.ID)
	}

	return ids, resp.Hits.Total.Value
}

func TestIndex_NestedAndJoin(t *testing.T) {
	indexName := "TestIndex_NestedAndJoin.index_1"
	index, err := NewIndex(indexName, "disk", 2)
	require.NoError(t, err)
	require.NoError(t, index.SetMappings(nestedIndexMappings(t)))
	require.NoError(t, StoreIndex(index))

	docs := map[string]map[string]interface{}{
		"q1": {
			"title": "question one",
			"comments": []interface{}{
				map[string]interface{}{"name": "alice", "stars": 5.0},
				map[string]interface{}{"name": "bob", "stars": 1.0},
			},
			"my_join": "question",
		},
		"q2": {
			"title": "question two",
			"comments": []interface{}{
				map[string]interface{}{"name": "bob", "stars": 5.0},
			},
			"my_join": "question",
		},
		"a1": {
			"title":   "answer one",
			"my_join": map[string]interface{}{"name": "answer", "parent": "q1"},
		},
		"a2": {
			"title":   "answer two",
			"my_join": map[string]interface{}{"name": "answer", "parent": "q2"},
		},
	}

	t.Run("Prepare", func(t *testing.T) {
		for docID, doc := range docs {
			assert.NoError(t, index.CreateDocument(docID, doc, false))
		}
		// three hidden nested documents are not counted
		waitForDocs(t, index, len(docs))
	})

	t.Run("nested documents are hidden", func(t *testing.T) {
		// root document holds no comments.* fields
		ids, total := searchIDs(t, index, map[string]interface{}{
			"term": map[string]interface{}{"comments.name": "alice"},
		})
		assert.Empty(t, ids)
		assert.Equal(t, 0, total)

		// hidden documents exist and are addressable by id
		hit, err := index.GetDocument("q1#comments#0")
		require.NoError(t, err)
		assert.Equal(t, "q1#comments#0", hit.ID)

		// root document counts its nested documents
		ids, _ = searchIDs(t, index, map[string]interface{}{
			"term": map[string]interface{}{"_nested_count.comments": 2.0},
		})
		assert.Equal(t, []string{"q1"}, ids)
	})

	t.Run("nested query", func(t *testing.T) {
		tests := []struct {
			name  string
			query map[string]interface{}
			want  []string
		}{
			{
				name: "single field matches both roots",
				query: map[string]interface{}{"nested": map[string]interface{}{
					"path":  "comments",
					"query": map[string]interface{}{"term": map[string]interface{}{"comments.name": "bob"}},
				}},
				want: []string{"q1", "q2"},
			},
			{
				name: "two fields of same comment",
				query: map[string]interface{}{"nested": map[string]interface{}{
					"path": "comments",
					"query": map[string]interface{}{"bool": map[string]interface{}{"must": []interface{}{
						map[string]interface{}{"term": map[string]interface{}{"comments.name": "bob"}},
						map[string]interface{}{"term": map[string]interface{}{"comments.stars": 5.0}},
					}}},
					"score_mode": "avg",
				}},
				want: []string{"q2"},
			},
			{
				name: "fields of different comments do not match",
				query: map[string]interface{}{"nested": map[string]interface{}{
					"path": "comments",
					"query": map[string]interface{}{"bool": map[string]interface{}{"must": []interface{}{
						map[string]interface{}{"term": map[string]interface{}{"comments.name": "alice"}},
						map[string]interface{}{"term": map[string]interface{}{"comments.stars": 1.0}},
					}}},
				}},
				want: []string{},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ids, total := searchIDs(t, index, tt.query)
				assert.ElementsMatch(t, tt.want, ids)
				assert.Equal(t, len(tt.want), total)
			})
		}
	})

	t.Run("join query", func(t *testing.T) {
		tests := []struct {
			name  string
			query map[string]interface{}
			want  []string
		}{
			{
				name: "has_child",
				query: map[string]interface{}{"has_child": map[string]interface{}{
					"type":  "answer",
					"query": map[string]interface{}{"match": map[string]interface{}{"title": "one"}},
				}},
				want: []string{"q1"},
			},
			{
				name: "has_child matches every parent",
				query: map[string]interface{}{"has_child": map[string]interface{}{
					"type":  "answer",
					"query": map[string]interface{}{"match_all": map[string]interface{}{}},
				}},
				want: []string{"q1", "q2"},
			},
			{
				name: "has_parent",
				query: map[string]interface{}{"has_parent": map[string]interface{}{
					"parent_type": "question",
					"query":       map[string]interface{}{"match": map[string]interface{}{"title": "two"}},
				}},
				want: []string{"a2"},
			},
			{
				name: "parent_id",
				query: map[string]interface{}{"parent_id": map[string]interface{}{
					"type": "answer",
					"id":   "q1",
				}},
				want: []string{"a1"},
			},
			{
				name: "has_child without match",
				query: map[string]interface{}{"has_child": map[string]interface{}{
					"type":  "answer",
					"query": map[string]interface{}{"match": map[string]interface{}{"title": "missing"}},
				}},
				want: []string{},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ids, total := searchIDs(t, index, tt.query)
				assert.ElementsMatch(t, tt.want, ids)
				assert.Equal(t, len(tt.want), total)
			})
		}
	})

	t.Run("update removes stale nested documents", func(t *testing.T) {
		doc := map[string]interface{}{
			"title": "question two",
			"comments": []interface{}{
				map[string]interface{}{"name": "carol", "stars": 3.0},
			},
			"my_join": "question",
		}
		require.NoError(t, index.UpdateDocument("q2", doc, false))

		assert.Eventually(t, func() bool {
			ids, _ := searchIDs(t, index, map[string]interface{}{"nested": map[string]interface{}{
				"path":  "comments",
				"query": map[string]interface{}{"term": map[string]interface{}{"comments.name": "carol"}},
			}})
			return len(ids) == 1 && ids[0] == "q2"
		}, 30*time.Second, 50*time.Millisecond)

		ids, _ := searchIDs(t, index, map[string]interface{}{"nested": map[string]interface{}{
			"path":  "comments",
			"query": map[string]interface{}{"term": map[string]interface{}{"comments.name": "bob"}},
		}})
		assert.Equal(t, []string{"q1"}, ids)
	})

	t.Run("delete removes nested documents", func(t *testing.T) {
		require.NoError(t, index.DeleteDocument("q1"))

		assert.Eventually(t, func() bool {
			_, err := index.GetDocument("q1#comments#0")
			return err == errors.ErrorIDNotFound
		}, 30*time.Second, 50*time.Millisecond, "hidden documents of deleted root must be removed")

		_, err := index.GetDocument("q1#comments#1")
		assert.Equal(t, errors.ErrorIDNotFound, err)

		ids, _ := searchIDs(t, index, map[string]interface{}{"nested": map[string]interface{}{
			"path":  "comments",
			"query": map[string]interface{}{"term": map[string]interface{}{"comments.name": "bob"}},
		}})
		assert.Empty(t, ids)
	})

	t.Run("Cleanup", func(t *testing.T) {
		assert.NoError(t, DeleteIndex(indexName))
	})
}

func TestIndex_JoinValidation(t *testing.T) {
	indexName := "TestIndex_JoinValidation.index_1"
	index, err := NewIndex(indexName, "disk", 1)
	require.NoError(t, err)
	require.NoError(t, index.SetMappings(nestedIndexMappings(t)))
	require.NoError(t, StoreIndex(index))

	tests := []struct {
		name    string
		doc     map[string]interface{}
		wantErr string
	}{
		{
			name:    "unknown relation",
			doc:     map[string]interface{}{"my_join": "comment"},
			wantErr: "unknown relation [comment]",
		},
		{
			name:    "child without parent",
			doc:     map[string]interface{}{"my_join": map[string]interface{}{"name": "answer"}},
			wantErr: "requires parent id",
		},
		{
			name:    "parent with parent id",
			doc:     map[string]interface{}{"my_join": map[string]interface{}{"name": "question", "parent": "q1"}},
			wantErr: "is not child relation",
		},
		{
			name:    "nested element is not object",
			doc:     map[string]interface{}{"comments": []interface{}{"alice"}},
			wantErr: "is not an object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := index.CreateDocument("doc-"+tt.name, tt.doc, false)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}

	assert.NoError(t, DeleteIndex(indexName))
}
