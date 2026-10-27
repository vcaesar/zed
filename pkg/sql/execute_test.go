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

package sql

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch/pkg/meta"
)

func TestFilterRowsHaving(t *testing.T) {
	columns := []ResultColumn{{Name: "level"}, {Name: "COUNT(*)"}}
	rows := [][]interface{}{
		{"error", int64(1)},
		{"warn", int64(2)},
		{"info", int64(3)},
	}

	tests := []struct {
		name string
		op   string
		want [][]interface{}
	}{
		{name: "equal", op: "=", want: [][]interface{}{{"warn", int64(2)}}},
		{name: "not equal", op: "!=", want: [][]interface{}{{"error", int64(1)}, {"info", int64(3)}}},
		{name: "less", op: "<", want: [][]interface{}{{"error", int64(1)}}},
		{name: "less equal", op: "<=", want: [][]interface{}{{"error", int64(1)}, {"warn", int64(2)}}},
		{name: "greater", op: ">", want: [][]interface{}{{"info", int64(3)}}},
		{name: "greater equal", op: ">=", want: [][]interface{}{{"warn", int64(2)}, {"info", int64(3)}}},
		{name: "unknown operator drops rows", op: "~", want: [][]interface{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt := &Statement{Having: []Condition{{Label: "COUNT(*)", Op: tt.op, Value: 2}}}
			assert.Equal(t, tt.want, filterRows(stmt, columns, rows))
		})
	}

	t.Run("non numeric cell never matches", func(t *testing.T) {
		stmt := &Statement{Having: []Condition{{Label: "level", Op: ">", Value: 0}}}
		assert.Empty(t, filterRows(stmt, columns, rows))
	})

	t.Run("no having keeps every row", func(t *testing.T) {
		assert.Equal(t, rows, filterRows(&Statement{}, columns, rows))
	})
}

func TestSortRows(t *testing.T) {
	columns := []ResultColumn{{Name: "level"}, {Name: "COUNT(*)"}}

	tests := []struct {
		name  string
		order []OrderKey
		want  [][]interface{}
	}{
		{
			name:  "numeric desc",
			order: []OrderKey{{Label: "COUNT(*)", Desc: true}},
			want: [][]interface{}{
				{"warn", int64(3)},
				{"error", int64(2)},
				{"info", int64(2)},
			},
		},
		{
			name:  "numeric then string",
			order: []OrderKey{{Label: "COUNT(*)"}, {Label: "level", Desc: true}},
			want: [][]interface{}{
				{"info", int64(2)},
				{"error", int64(2)},
				{"warn", int64(3)},
			},
		},
		{
			name:  "unknown column is ignored",
			order: []OrderKey{{Label: "missing"}},
			want: [][]interface{}{
				{"error", int64(2)},
				{"warn", int64(3)},
				{"info", int64(2)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := [][]interface{}{
				{"error", int64(2)},
				{"warn", int64(3)},
				{"info", int64(2)},
			}
			sortRows(&Statement{Order: tt.order}, columns, rows)
			assert.Equal(t, tt.want, rows)
		})
	}
}

func TestGroupValue(t *testing.T) {
	stamp := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		group *GroupKey
		key   interface{}
		want  interface{}
	}{
		{
			name:  "date histogram millis",
			group: &GroupKey{Kind: GroupDateHistogram},
			key:   stamp.UnixMilli(),
			want:  "2024-03-01T00:00:00Z",
		},
		{
			name:  "date histogram non numeric key",
			group: &GroupKey{Kind: GroupDateHistogram},
			key:   "2024-03-01",
			want:  "2024-03-01",
		},
		{name: "terms", group: &GroupKey{Kind: GroupTerms}, key: "error", want: "error"},
		{name: "histogram", group: &GroupKey{Kind: GroupHistogram}, key: int64(10), want: int64(10)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, groupValue(tt.group, tt.key))
		})
	}
}

func TestMetricValue(t *testing.T) {
	tests := []struct {
		name string
		agg  meta.AggregationResponse
		kind string
		want interface{}
	}{
		{name: "average keeps float", agg: meta.AggregationResponse{Value: 1.5}, kind: AggAvg, want: 1.5},
		{name: "value count narrows", agg: meta.AggregationResponse{Value: 3.0}, kind: AggCount, want: int64(3)},
		{name: "cardinality narrows", agg: meta.AggregationResponse{Value: 2.0}, kind: AggCountDistinct, want: int64(2)},
		{name: "missing value", agg: meta.AggregationResponse{}, kind: AggSum, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, metricValue(tt.agg, tt.kind))
		})
	}
}

func TestColumnType(t *testing.T) {
	tests := []struct {
		name string
		typ  string
		want string
	}{
		{name: "text", typ: "text", want: "text"},
		{name: "keyword", typ: "keyword", want: "keyword"},
		{name: "numeric", typ: "numeric", want: "double"},
		{name: "date", typ: "date", want: "datetime"},
		{name: "bool", typ: "bool", want: "boolean"},
		{name: "unmapped", typ: "", want: "keyword"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, columnType(tt.typ))
		})
	}
}

func TestAggColumnType(t *testing.T) {
	fields := FieldTypes{"level": "keyword", "count": "numeric"}
	tests := []struct {
		name   string
		stmt   *Statement
		column Column
		want   string
	}{
		{
			name:   "date histogram key",
			stmt:   &Statement{Group: &GroupKey{Kind: GroupDateHistogram}},
			column: Column{Group: true},
			want:   "datetime",
		},
		{
			name:   "histogram key",
			stmt:   &Statement{Group: &GroupKey{Kind: GroupHistogram}},
			column: Column{Group: true},
			want:   "double",
		},
		{
			name:   "terms key",
			stmt:   &Statement{Group: &GroupKey{Kind: GroupTerms}},
			column: Column{Group: true, Field: "level"},
			want:   "keyword",
		},
		{
			name:   "count is long",
			stmt:   &Statement{},
			column: Column{Agg: AggCountStar},
			want:   "long",
		},
		{
			name:   "metric is double",
			stmt:   &Statement{},
			column: Column{Agg: AggAvg, Field: "count"},
			want:   "double",
		},
		{
			name:   "plain column keeps mapping type",
			stmt:   &Statement{},
			column: Column{Field: "count"},
			want:   "double",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, aggColumnType(tt.stmt, tt.column, fields))
		})
	}
}

func TestMatchesAny(t *testing.T) {
	tests := []struct {
		name     string
		index    string
		patterns []string
		want     bool
	}{
		{name: "exact", index: "logs", patterns: []string{"logs"}, want: true},
		{name: "prefix wildcard", index: "logs-2024", patterns: []string{"logs-*"}, want: true},
		{name: "suffix wildcard", index: "app-logs", patterns: []string{"*-logs"}, want: true},
		{name: "second pattern", index: "metrics", patterns: []string{"logs", "metrics"}, want: true},
		{name: "no match", index: "metrics", patterns: []string{"logs-*"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matchesAny(tt.index, tt.patterns))
		})
	}
}

func TestSourceFields(t *testing.T) {
	sources := []map[string]interface{}{
		{"b": 1, "a": 2},
		{"c": 3, "a": 4},
	}
	assert.Equal(t, []string{"a", "b", "c"}, sourceFields(sources))
}

func TestLiteralValues(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want interface{}
	}{
		{name: "string", sql: `SELECT * FROM t WHERE a = 'x'`, want: "x"},
		{name: "integer", sql: `SELECT * FROM t WHERE a = 5`, want: int64(5)},
		{name: "decimal", sql: `SELECT * FROM t WHERE a = 1.25`, want: 1.25},
		{name: "negative", sql: `SELECT * FROM t WHERE a = -2`, want: -2.0},
		{name: "boolean", sql: `SELECT * FROM t WHERE a = true`, want: true},
		{name: "null", sql: `SELECT * FROM t WHERE a = null`, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, err := Parse(tt.sql)
			require.NoError(t, err)
			dsl, err := BuildDSL(stmt, FieldTypes{}, 0)
			require.NoError(t, err)

			query, ok := dsl["query"].(map[string]interface{})
			require.True(t, ok)
			term, ok := query["term"].(map[string]interface{})
			require.True(t, ok, "query is %v", query)
			field, ok := term["a"].(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, tt.want, field["value"])
		})
	}
}
