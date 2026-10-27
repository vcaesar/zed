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

package query

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vcaesar/riot"

	"github.com/zincsearch/zincsearch/pkg/meta"
)

// tagTerm returns term query of boolean clause on hidden field.
func tagTerm(t *testing.T, q riot.Query, want map[string]string) {
	t.Helper()

	bq, ok := q.(*riot.BooleanQuery)
	require.True(t, ok)
	got := make(map[string]string, len(want))
	for _, must := range bq.Musts() {
		if tq, ok := must.(*riot.TermQuery); ok {
			got[tq.Field()] = tq.Term()
		}
	}
	for field, value := range want {
		assert.Equal(t, value, got[field], field)
	}
}

func TestNestedQuery(t *testing.T) {
	mappings := testMappings()

	t.Run("nested", func(t *testing.T) {
		q, err := NestedQuery(map[string]interface{}{
			"path":       "comments",
			"query":      termOn("tag", "a"),
			"score_mode": "avg",
		}, mappings, nil)
		require.NoError(t, err)
		tagTerm(t, q, map[string]string{meta.NestedPathField: "comments"})
	})

	tests := []struct {
		name    string
		query   map[string]interface{}
		wantErr string
	}{
		{
			name:    "missing path",
			query:   map[string]interface{}{"query": termOn("tag", "a")},
			wantErr: "[nested] requires [path] query",
		},
		{
			name:    "missing query",
			query:   map[string]interface{}{"path": "comments"},
			wantErr: "[nested] requires [query] query",
		},
		{
			name:    "bad query type",
			query:   map[string]interface{}{"path": "comments", "query": "a"},
			wantErr: "doesn't support values of type",
		},
		{
			name:    "unknown score_mode",
			query:   map[string]interface{}{"path": "comments", "query": termOn("tag", "a"), "score_mode": "nope"},
			wantErr: "unknown score_mode [nope]",
		},
		{
			name:    "unknown field",
			query:   map[string]interface{}{"path": "comments", "query": termOn("tag", "a"), "nope": 1},
			wantErr: "[nested] unknown field [nope]",
		},
		{
			name:    "bad inner query",
			query:   map[string]interface{}{"path": "comments", "query": map[string]interface{}{"nope": map[string]interface{}{}}},
			wantErr: "[nested] failed to parse field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := NestedQuery(tt.query, mappings, nil)
			assert.Nil(t, q)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestJoinQueries(t *testing.T) {
	mappings := testMappings()

	t.Run("has_child", func(t *testing.T) {
		q, err := HasChildQuery(map[string]interface{}{
			"type":         "answer",
			"query":        termOn("tag", "a"),
			"score_mode":   "none",
			"min_children": 1.0,
			"max_children": 5.0,
		}, mappings, nil)
		require.NoError(t, err)
		tagTerm(t, q, map[string]string{meta.JoinNameField: "answer"})
	})

	t.Run("has_parent", func(t *testing.T) {
		q, err := HasParentQuery(map[string]interface{}{
			"parent_type": "question",
			"query":       termOn("tag", "a"),
			"score":       true,
		}, mappings, nil)
		require.NoError(t, err)
		tagTerm(t, q, map[string]string{meta.JoinNameField: "question"})
	})

	t.Run("parent_id", func(t *testing.T) {
		q, err := ParentIDQuery(map[string]interface{}{"type": "answer", "id": "q1"})
		require.NoError(t, err)
		tagTerm(t, q, map[string]string{
			meta.JoinNameField:   "answer",
			meta.JoinParentField: "q1",
		})
	})

	tests := []struct {
		name    string
		parse   func(map[string]interface{}) (riot.Query, error)
		query   map[string]interface{}
		wantErr string
	}{
		{
			name:    "has_child without type",
			parse:   func(q map[string]interface{}) (riot.Query, error) { return HasChildQuery(q, mappings, nil) },
			query:   map[string]interface{}{"query": termOn("tag", "a")},
			wantErr: "[has_child] requires [type] query",
		},
		{
			name:    "has_child without query",
			parse:   func(q map[string]interface{}) (riot.Query, error) { return HasChildQuery(q, mappings, nil) },
			query:   map[string]interface{}{"type": "answer"},
			wantErr: "[has_child] requires [query] query",
		},
		{
			name:    "has_child unknown score_mode",
			parse:   func(q map[string]interface{}) (riot.Query, error) { return HasChildQuery(q, mappings, nil) },
			query:   map[string]interface{}{"type": "answer", "query": termOn("tag", "a"), "score_mode": "nope"},
			wantErr: "unknown score_mode [nope]",
		},
		{
			name:    "has_child unknown field",
			parse:   func(q map[string]interface{}) (riot.Query, error) { return HasChildQuery(q, mappings, nil) },
			query:   map[string]interface{}{"type": "answer", "query": termOn("tag", "a"), "nope": 1},
			wantErr: "[has_child] unknown field [nope]",
		},
		{
			name:    "has_parent without parent_type",
			parse:   func(q map[string]interface{}) (riot.Query, error) { return HasParentQuery(q, mappings, nil) },
			query:   map[string]interface{}{"query": termOn("tag", "a")},
			wantErr: "[has_parent] requires [parent_type] query",
		},
		{
			name:    "has_parent bad query",
			parse:   func(q map[string]interface{}) (riot.Query, error) { return HasParentQuery(q, mappings, nil) },
			query:   map[string]interface{}{"parent_type": "question", "query": map[string]interface{}{"nope": map[string]interface{}{}}},
			wantErr: "[has_parent] failed to parse field",
		},
		{
			name:    "parent_id without type",
			parse:   func(q map[string]interface{}) (riot.Query, error) { return ParentIDQuery(q) },
			query:   map[string]interface{}{"id": "q1"},
			wantErr: "[parent_id] requires [type] query",
		},
		{
			name:    "parent_id without id",
			parse:   func(q map[string]interface{}) (riot.Query, error) { return ParentIDQuery(q) },
			query:   map[string]interface{}{"type": "answer"},
			wantErr: "[parent_id] requires [id] query",
		},
		{
			name:    "parent_id unknown field",
			parse:   func(q map[string]interface{}) (riot.Query, error) { return ParentIDQuery(q) },
			query:   map[string]interface{}{"type": "answer", "id": "q1", "nope": 1},
			wantErr: "[parent_id] unknown field [nope]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := tt.parse(tt.query)
			assert.Nil(t, q)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// TestNestedJoinQuery_Dispatch checks that clauses are reachable from Query().
func TestNestedJoinQuery_Dispatch(t *testing.T) {
	mappings := testMappings()

	tests := []struct {
		name  string
		query map[string]interface{}
	}{
		{name: "nested", query: map[string]interface{}{"nested": map[string]interface{}{"path": "comments", "query": termOn("tag", "a")}}},
		{name: "has_child", query: map[string]interface{}{"has_child": map[string]interface{}{"type": "answer", "query": termOn("tag", "a")}}},
		{name: "has_parent", query: map[string]interface{}{"has_parent": map[string]interface{}{"parent_type": "question", "query": termOn("tag", "a")}}},
		{name: "parent_id", query: map[string]interface{}{"parent_id": map[string]interface{}{"type": "answer", "id": "q1"}}},
		{name: "function_score", query: map[string]interface{}{"function_score": map[string]interface{}{"query": termOn("tag", "a"), "weight": 2.0}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := Query(tt.query, mappings, nil)
			require.NoError(t, err)
			assert.NotNil(t, q)
		})
	}

	t.Run("parse error is wrapped", func(t *testing.T) {
		_, err := Query(map[string]interface{}{"nested": map[string]interface{}{"path": "comments"}}, mappings, nil)
		assert.ErrorContains(t, err, "[nested] failed to parse field")
	})
}
