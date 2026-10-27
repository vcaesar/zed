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

package uquery

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch/pkg/meta"
)

// execCall records single invocation of exec function.
type execCall struct {
	sub          map[string]interface{}
	collectField string
}

// fakeExec returns exec function answering with values and recording calls.
func fakeExec(calls *[]execCall, values ...string) ExecFunc {
	return func(sub map[string]interface{}, collectField string) ([]string, error) {
		*calls = append(*calls, execCall{sub: sub, collectField: collectField})
		return values, nil
	}
}

func matchQuery(field, value string) map[string]interface{} {
	return map[string]interface{}{"match": map[string]interface{}{field: value}}
}

func TestRewrite_Clauses(t *testing.T) {
	tests := []struct {
		name         string
		query        map[string]interface{}
		values       []string
		wantField    string
		wantSub      map[string]interface{}
		wantRewrite  map[string]interface{}
		wantExecCall bool
	}{
		{
			name:  "nested",
			query: map[string]interface{}{"nested": map[string]interface{}{"path": "comments", "query": matchQuery("comments.name", "john"), "score_mode": "avg"}},
			// same root matched twice, ids are deduplicated
			values:    []string{"1", "2", "1"},
			wantField: meta.NestedParentField,
			wantSub: map[string]interface{}{"bool": map[string]interface{}{
				"must":   matchQuery("comments.name", "john"),
				"filter": map[string]interface{}{"term": map[string]interface{}{meta.NestedPathField: "comments"}},
			}},
			wantRewrite:  map[string]interface{}{"terms": map[string]interface{}{"_id": []string{"1", "2"}}},
			wantExecCall: true,
		},
		{
			name:      "nested without match",
			query:     map[string]interface{}{"nested": map[string]interface{}{"path": "comments", "query": matchQuery("comments.name", "john")}},
			values:    nil,
			wantField: meta.NestedParentField,
			wantSub: map[string]interface{}{"bool": map[string]interface{}{
				"must":   matchQuery("comments.name", "john"),
				"filter": map[string]interface{}{"term": map[string]interface{}{meta.NestedPathField: "comments"}},
			}},
			wantRewrite:  map[string]interface{}{"match_none": map[string]interface{}{}},
			wantExecCall: true,
		},
		{
			name:      "has_child",
			query:     map[string]interface{}{"has_child": map[string]interface{}{"type": "answer", "query": matchQuery("body", "go"), "score_mode": "none"}},
			values:    []string{"Q1", "Q2"},
			wantField: meta.JoinParentField,
			wantSub: map[string]interface{}{"bool": map[string]interface{}{
				"must":   matchQuery("body", "go"),
				"filter": map[string]interface{}{"term": map[string]interface{}{meta.JoinNameField: "answer"}},
			}},
			wantRewrite:  map[string]interface{}{"terms": map[string]interface{}{"_id": []string{"Q1", "Q2"}}},
			wantExecCall: true,
		},
		{
			name:      "has_child min_children",
			query:     map[string]interface{}{"has_child": map[string]interface{}{"type": "answer", "query": matchQuery("body", "go"), "min_children": 2.0}},
			values:    []string{"Q1", "Q2", "Q1"},
			wantField: meta.JoinParentField,
			wantSub: map[string]interface{}{"bool": map[string]interface{}{
				"must":   matchQuery("body", "go"),
				"filter": map[string]interface{}{"term": map[string]interface{}{meta.JoinNameField: "answer"}},
			}},
			wantRewrite:  map[string]interface{}{"terms": map[string]interface{}{"_id": []string{"Q1"}}},
			wantExecCall: true,
		},
		{
			name:      "has_child max_children",
			query:     map[string]interface{}{"has_child": map[string]interface{}{"type": "answer", "query": matchQuery("body", "go"), "max_children": 1.0}},
			values:    []string{"Q1", "Q2", "Q1"},
			wantField: meta.JoinParentField,
			wantSub: map[string]interface{}{"bool": map[string]interface{}{
				"must":   matchQuery("body", "go"),
				"filter": map[string]interface{}{"term": map[string]interface{}{meta.JoinNameField: "answer"}},
			}},
			wantRewrite:  map[string]interface{}{"terms": map[string]interface{}{"_id": []string{"Q2"}}},
			wantExecCall: true,
		},
		{
			name:      "has_parent",
			query:     map[string]interface{}{"has_parent": map[string]interface{}{"parent_type": "question", "query": matchQuery("title", "go"), "score": true}},
			values:    []string{"Q1"},
			wantField: "_id",
			wantSub: map[string]interface{}{"bool": map[string]interface{}{
				"must":   matchQuery("title", "go"),
				"filter": map[string]interface{}{"term": map[string]interface{}{meta.JoinNameField: "question"}},
			}},
			wantRewrite:  map[string]interface{}{"terms": map[string]interface{}{meta.JoinParentField: []string{"Q1"}}},
			wantExecCall: true,
		},
		{
			name:  "parent_id",
			query: map[string]interface{}{"parent_id": map[string]interface{}{"type": "answer", "id": "Q1"}},
			wantRewrite: map[string]interface{}{"bool": map[string]interface{}{
				"filter": []interface{}{
					map[string]interface{}{"term": map[string]interface{}{meta.JoinNameField: "answer"}},
					map[string]interface{}{"term": map[string]interface{}{meta.JoinParentField: "Q1"}},
				},
			}},
		},
		{
			name:        "untouched query",
			query:       matchQuery("name", "john"),
			wantRewrite: matchQuery("name", "john"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []execCall
			got, err := Rewrite(tt.query, fakeExec(&calls, tt.values...))
			require.NoError(t, err)
			assert.Equal(t, tt.wantRewrite, got)
			if !tt.wantExecCall {
				assert.Empty(t, calls)
				return
			}
			require.Len(t, calls, 1)
			assert.Equal(t, tt.wantField, calls[0].collectField)
			assert.Equal(t, tt.wantSub, calls[0].sub)
		})
	}
}

func TestRewrite_Recursion(t *testing.T) {
	nested := map[string]interface{}{"nested": map[string]interface{}{"path": "comments", "query": matchQuery("comments.name", "john")}}
	wantIDs := map[string]interface{}{"terms": map[string]interface{}{"_id": []string{"1"}}}

	tests := []struct {
		name  string
		query map[string]interface{}
		want  map[string]interface{}
	}{
		{
			name:  "bool object clause",
			query: map[string]interface{}{"bool": map[string]interface{}{"must": nested}},
			want:  map[string]interface{}{"bool": map[string]interface{}{"must": wantIDs}},
		},
		{
			name:  "bool array clause",
			query: map[string]interface{}{"bool": map[string]interface{}{"filter": []interface{}{nested, matchQuery("name", "john")}}},
			want: map[string]interface{}{"bool": map[string]interface{}{"filter": []interface{}{
				wantIDs, matchQuery("name", "john"),
			}}},
		},
		{
			name:  "constant_score",
			query: map[string]interface{}{"constant_score": map[string]interface{}{"filter": nested, "boost": 2.0}},
			want:  map[string]interface{}{"constant_score": map[string]interface{}{"filter": wantIDs, "boost": 2.0}},
		},
		{
			name:  "dis_max",
			query: map[string]interface{}{"dis_max": map[string]interface{}{"queries": []interface{}{nested}}},
			want:  map[string]interface{}{"dis_max": map[string]interface{}{"queries": []interface{}{wantIDs}}},
		},
		{
			name: "function_score query and filter",
			query: map[string]interface{}{"function_score": map[string]interface{}{
				"query":     nested,
				"functions": []interface{}{map[string]interface{}{"filter": nested, "weight": 2.0}},
			}},
			want: map[string]interface{}{"function_score": map[string]interface{}{
				"query":     wantIDs,
				"functions": []interface{}{map[string]interface{}{"filter": wantIDs, "weight": 2.0}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []execCall
			got, err := Rewrite(tt.query, fakeExec(&calls, "1"))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.NotEmpty(t, calls)
		})
	}
}

func TestRewrite_Errors(t *testing.T) {
	tests := []struct {
		name    string
		query   map[string]interface{}
		wantErr string
	}{
		{
			name:    "nested without path",
			query:   map[string]interface{}{"nested": map[string]interface{}{"query": matchQuery("a", "b")}},
			wantErr: "[nested] requires [path] field",
		},
		{
			name:    "nested without query",
			query:   map[string]interface{}{"nested": map[string]interface{}{"path": "comments"}},
			wantErr: "[nested] requires [query] field",
		},
		{
			name:    "nested unknown field",
			query:   map[string]interface{}{"nested": map[string]interface{}{"path": "comments", "query": matchQuery("a", "b"), "nope": 1}},
			wantErr: "[nested] unknown field [nope]",
		},
		{
			name:    "nested bad type",
			query:   map[string]interface{}{"nested": "comments"},
			wantErr: "[nested] query doesn't support value type string",
		},
		{
			name:    "has_child without type",
			query:   map[string]interface{}{"has_child": map[string]interface{}{"query": matchQuery("a", "b")}},
			wantErr: "[has_child] requires [type] field",
		},
		{
			name:    "has_parent without query",
			query:   map[string]interface{}{"has_parent": map[string]interface{}{"parent_type": "question"}},
			wantErr: "[has_parent] requires [query] field",
		},
		{
			name:    "parent_id without id",
			query:   map[string]interface{}{"parent_id": map[string]interface{}{"type": "answer"}},
			wantErr: "[parent_id] requires [id] field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []execCall
			_, err := Rewrite(tt.query, fakeExec(&calls, "1"))
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestRewrite_TooManyMatches(t *testing.T) {
	limit := RewriteLimit()
	ids := make([]string, limit+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("id-%d", i)
	}

	var calls []execCall
	_, err := Rewrite(map[string]interface{}{
		"nested": map[string]interface{}{"path": "comments", "query": matchQuery("comments.name", "john")},
	}, fakeExec(&calls, ids...))
	assert.ErrorContains(t, err, "too many matches")
}

func TestRewrite_ExecError(t *testing.T) {
	_, err := Rewrite(map[string]interface{}{
		"nested": map[string]interface{}{"path": "comments", "query": matchQuery("comments.name", "john")},
	}, func(_ map[string]interface{}, _ string) ([]string, error) {
		return nil, fmt.Errorf("reader closed")
	})
	assert.ErrorContains(t, err, "reader closed")
}

func TestRewrite_QueryTypes(t *testing.T) {
	t.Run("nil query", func(t *testing.T) {
		got, err := Rewrite(nil, fakeExec(&[]execCall{}))
		assert.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("meta query", func(t *testing.T) {
		var calls []execCall
		query := &meta.Query{Nested: &meta.NestedQuery{
			Path:      "comments",
			Query:     matchQuery("comments.name", "john"),
			ScoreMode: "avg",
		}}
		got, err := Rewrite(query, fakeExec(&calls, "1"))
		require.NoError(t, err)
		assert.Equal(t, map[string]interface{}{"terms": map[string]interface{}{"_id": []string{"1"}}}, got)
		require.Len(t, calls, 1)
		assert.Equal(t, meta.NestedParentField, calls[0].collectField)
	})
}
