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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

// testFields is the mapping every translate test is written against.
var testFields = FieldTypes{
	"message": "text",
	"level":   "keyword",
	"count":   "numeric",
	"price":   "numeric",
	"ok":      "bool",
	"ts":      "date",
}

func translateDSL(t *testing.T, sql string) map[string]interface{} {
	t.Helper()
	stmt, err := Parse(sql)
	require.NoError(t, err)
	dsl, err := BuildDSL(stmt, testFields, 0)
	require.NoError(t, err)
	return dsl
}

// assertDSL compares a DSL document against its expected JSON, ignoring the
// Go types the numbers happen to have.
func assertDSL(t *testing.T, expected string, actual map[string]interface{}) {
	t.Helper()
	data, err := json.Marshal(actual)
	require.NoError(t, err)
	var got, want map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &got))
	require.NoError(t, json.Unmarshal([]byte(expected), &want))
	assert.Equal(t, want, got)
}

func TestTranslateSelect(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "select star",
			sql:  `SELECT * FROM logs`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"from":0,"size":1000}`,
		},
		{
			name: "select columns",
			sql:  `SELECT level, count FROM logs`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"from":0,"size":1000,
				"_source":["level","count"]}`,
		},
		{
			name: "quoted index and column",
			sql:  `SELECT "level" FROM "logs-2024"`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"from":0,"size":1000,
				"_source":["level"]}`,
		},
		{
			name: "order by and limit",
			sql:  `SELECT level FROM logs ORDER BY level DESC, count ASC LIMIT 5 OFFSET 2`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"from":2,"size":5,
				"_source":["level"],
				"sort":[{"level":{"order":"desc"}},{"count":{"order":"asc"}}]}`,
		},
		{
			name: "limit offset comma form",
			sql:  `SELECT level FROM logs LIMIT 2, 5`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"from":2,"size":5,
				"_source":["level"]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertDSL(t, tt.want, translateDSL(t, tt.sql))
		})
	}
}

func TestTranslateWhere(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "equals text uses match",
			sql:  `SELECT * FROM logs WHERE message = 'hello world'`,
			want: `{"match":{"message":{"query":"hello world"}}}`,
		},
		{
			name: "equals keyword uses term",
			sql:  `SELECT * FROM logs WHERE level = 'error'`,
			want: `{"term":{"level":{"value":"error"}}}`,
		},
		{
			name: "equals numeric uses term",
			sql:  `SELECT * FROM logs WHERE count = 5`,
			want: `{"term":{"count":{"value":5}}}`,
		},
		{
			name: "equals boolean uses term",
			sql:  `SELECT * FROM logs WHERE ok = true`,
			want: `{"term":{"ok":{"value":true}}}`,
		},
		{
			name: "equals unmapped field uses term",
			sql:  `SELECT * FROM logs WHERE other = 'x'`,
			want: `{"term":{"other":{"value":"x"}}}`,
		},
		{
			name: "not equals",
			sql:  `SELECT * FROM logs WHERE level != 'error'`,
			want: `{"bool":{"must_not":[{"term":{"level":{"value":"error"}}}]}}`,
		},
		{
			name: "not equals angle form",
			sql:  `SELECT * FROM logs WHERE level <> 'error'`,
			want: `{"bool":{"must_not":[{"term":{"level":{"value":"error"}}}]}}`,
		},
		{
			name: "less than",
			sql:  `SELECT * FROM logs WHERE count < 10`,
			want: `{"range":{"count":{"lt":10}}}`,
		},
		{
			name: "less or equal",
			sql:  `SELECT * FROM logs WHERE count <= 10`,
			want: `{"range":{"count":{"lte":10}}}`,
		},
		{
			name: "greater than",
			sql:  `SELECT * FROM logs WHERE price > 1.5`,
			want: `{"range":{"price":{"gt":1.5}}}`,
		},
		{
			name: "greater or equal on date",
			sql:  `SELECT * FROM logs WHERE ts >= '2024-01-01T00:00:00Z'`,
			want: `{"range":{"ts":{"gte":"2024-01-01T00:00:00Z"}}}`,
		},
		{
			name: "between",
			sql:  `SELECT * FROM logs WHERE count BETWEEN 1 AND 5`,
			want: `{"range":{"count":{"gte":1,"lte":5}}}`,
		},
		{
			name: "not between",
			sql:  `SELECT * FROM logs WHERE count NOT BETWEEN 1 AND 5`,
			want: `{"bool":{"must_not":[{"range":{"count":{"gte":1,"lte":5}}}]}}`,
		},
		{
			name: "in",
			sql:  `SELECT * FROM logs WHERE level IN ('error', 'warn')`,
			want: `{"terms":{"level":["error","warn"]}}`,
		},
		{
			name: "not in",
			sql:  `SELECT * FROM logs WHERE count NOT IN (1, 2)`,
			want: `{"bool":{"must_not":[{"terms":{"count":[1,2]}}]}}`,
		},
		{
			name: "like",
			sql:  `SELECT * FROM logs WHERE level LIKE 'err%_'`,
			want: `{"wildcard":{"level":{"value":"err*?"}}}`,
		},
		{
			name: "not like",
			sql:  `SELECT * FROM logs WHERE level NOT LIKE 'err%'`,
			want: `{"bool":{"must_not":[{"wildcard":{"level":{"value":"err*"}}}]}}`,
		},
		{
			name: "is null",
			sql:  `SELECT * FROM logs WHERE level IS NULL`,
			want: `{"bool":{"must_not":[{"exists":{"field":"level"}}]}}`,
		},
		{
			name: "is not null",
			sql:  `SELECT * FROM logs WHERE level IS NOT NULL`,
			want: `{"exists":{"field":"level"}}`,
		},
		{
			name: "and flattens",
			sql:  `SELECT * FROM logs WHERE level = 'error' AND count > 1 AND ok = false`,
			want: `{"bool":{"must":[
				{"term":{"level":{"value":"error"}}},
				{"range":{"count":{"gt":1}}},
				{"term":{"ok":{"value":false}}}]}}`,
		},
		{
			name: "or flattens",
			sql:  `SELECT * FROM logs WHERE level = 'error' OR level = 'warn'`,
			want: `{"bool":{"should":[
				{"term":{"level":{"value":"error"}}},
				{"term":{"level":{"value":"warn"}}}],
				"minimum_should_match":1}}`,
		},
		{
			name: "not",
			sql:  `SELECT * FROM logs WHERE NOT (level = 'error' OR count > 1)`,
			want: `{"bool":{"must_not":[{"bool":{"should":[
				{"term":{"level":{"value":"error"}}},
				{"range":{"count":{"gt":1}}}],
				"minimum_should_match":1}}]}}`,
		},
		{
			name: "nested and or",
			sql:  `SELECT * FROM logs WHERE level = 'error' AND (count > 1 OR ok = true)`,
			want: `{"bool":{"must":[
				{"term":{"level":{"value":"error"}}},
				{"bool":{"should":[
					{"range":{"count":{"gt":1}}},
					{"term":{"ok":{"value":true}}}],
					"minimum_should_match":1}}]}}`,
		},
		{
			name: "match function",
			sql:  `SELECT * FROM logs WHERE MATCH(message, 'hello world')`,
			want: `{"match":{"message":{"query":"hello world"}}}`,
		},
		{
			name: "query function",
			sql:  `SELECT * FROM logs WHERE QUERY('level:error AND count:>1')`,
			want: `{"query_string":{"query":"level:error AND count:>1"}}`,
		},
		{
			name: "match combined with filter",
			sql:  `SELECT * FROM logs WHERE MATCH(message, 'hello') AND level = 'error'`,
			want: `{"bool":{"must":[
				{"match":{"message":{"query":"hello"}}},
				{"term":{"level":{"value":"error"}}}]}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsl := translateDSL(t, tt.sql)
			query, ok := dsl["query"].(map[string]interface{})
			require.True(t, ok, "query is %T", dsl["query"])
			assertDSL(t, tt.want, query)
		})
	}
}

func TestTranslateAggregations(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "count star has no aggregation",
			sql:  `SELECT COUNT(*) FROM logs`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"size":0}`,
		},
		{
			name: "count star with filter",
			sql:  `SELECT COUNT(*) FROM logs WHERE level = 'error'`,
			want: `{"query":{"term":{"level":{"value":"error"}}},"track_total_hits":true,"size":0}`,
		},
		{
			name: "metrics without group by",
			sql:  `SELECT AVG(price), SUM(price), MIN(count), MAX(count) FROM logs`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"size":0,"aggs":{
				"AVG(price)":{"avg":{"field":"price"}},
				"SUM(price)":{"sum":{"field":"price"}},
				"MIN(count)":{"min":{"field":"count"}},
				"MAX(count)":{"max":{"field":"count"}}}}`,
		},
		{
			name: "count field and count distinct",
			sql:  `SELECT COUNT(count), COUNT(DISTINCT level) FROM logs`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"size":0,"aggs":{
				"COUNT(count)":{"value_count":{"field":"count"}},
				"COUNT(DISTINCT level)":{"cardinality":{"field":"level"}}}}`,
		},
		{
			name: "group by terms with metrics",
			sql:  `SELECT level, COUNT(*), AVG(price) AS avg_price FROM logs GROUP BY level`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"size":0,"aggs":{
				"groupby":{"terms":{"field":"level","size":1000},
				"aggs":{"avg_price":{"avg":{"field":"price"}}}}}}`,
		},
		{
			name: "group by terms keeps every bucket, limit applies to rows",
			sql:  `SELECT level, COUNT(*) FROM logs GROUP BY level LIMIT 3`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"size":0,"aggs":{
				"groupby":{"terms":{"field":"level","size":1000}}}}`,
		},
		{
			name: "group by date histogram calendar interval",
			sql:  `SELECT DATE_HISTOGRAM(ts, '1d'), COUNT(*) FROM logs GROUP BY DATE_HISTOGRAM(ts, '1d')`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"size":0,"aggs":{
				"groupby":{"date_histogram":{"field":"ts","calendar_interval":"1d","size":1000}}}}`,
		},
		{
			name: "group by date histogram fixed interval",
			sql:  `SELECT COUNT(*) FROM logs GROUP BY DATE_HISTOGRAM(ts, '30m')`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"size":0,"aggs":{
				"groupby":{"date_histogram":{"field":"ts","fixed_interval":"30m","size":1000}}}}`,
		},
		{
			name: "group by histogram",
			sql:  `SELECT HISTOGRAM(price, 10), COUNT(*) FROM logs GROUP BY HISTOGRAM(price, 10)`,
			want: `{"query":{"match_all":{}},"track_total_hits":true,"size":0,"aggs":{
				"groupby":{"histogram":{"field":"price","interval":10,"size":1000}}}}`,
		},
		{
			name: "group by with where and having",
			sql: `SELECT level, COUNT(*) FROM logs WHERE ok = true GROUP BY level
				HAVING COUNT(*) > 2 ORDER BY COUNT(*) DESC`,
			want: `{"query":{"term":{"ok":{"value":true}}},"track_total_hits":true,"size":0,"aggs":{
				"groupby":{"terms":{"field":"level","size":1000}}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertDSL(t, tt.want, translateDSL(t, tt.sql))
		})
	}
}

func TestTranslateFetchSize(t *testing.T) {
	stmt, err := Parse(`SELECT level FROM logs LIMIT 100`)
	require.NoError(t, err)

	dsl, err := BuildDSL(stmt, testFields, 10)
	require.NoError(t, err)
	assert.Equal(t, 10, dsl["size"])

	dsl, err = BuildDSL(stmt, testFields, 500)
	require.NoError(t, err)
	assert.Equal(t, 100, dsl["size"])
}

func TestParseStatement(t *testing.T) {
	t.Run("indexes", func(t *testing.T) {
		stmt, err := Parse(`SELECT * FROM logs, "logs-2024", ` + "`metrics-*`")
		require.NoError(t, err)
		assert.Equal(t, []string{"logs", "logs-2024", "metrics-*"}, stmt.Indexes)
		assert.True(t, stmt.Star)
		assert.False(t, stmt.Aggregated)
	})

	t.Run("columns and aliases", func(t *testing.T) {
		stmt, err := Parse(`SELECT level AS lvl, COUNT(*) AS total FROM logs GROUP BY level`)
		require.NoError(t, err)
		require.Len(t, stmt.Columns, 2)
		assert.Equal(t, Column{Label: "lvl", Field: "level", Group: true}, stmt.Columns[0])
		assert.Equal(t, Column{Label: "total", Agg: AggCountStar}, stmt.Columns[1])
		assert.True(t, stmt.Aggregated)
	})

	t.Run("having and order", func(t *testing.T) {
		stmt, err := Parse(`SELECT level, COUNT(*) FROM logs GROUP BY level
			HAVING COUNT(*) >= 2 ORDER BY COUNT(*) DESC`)
		require.NoError(t, err)
		assert.Equal(t, []Condition{{Label: "COUNT(*)", Op: ">=", Value: 2}}, stmt.Having)
		assert.Equal(t, []OrderKey{{Label: "COUNT(*)", Desc: true}}, stmt.Order)
	})

	t.Run("having on alias", func(t *testing.T) {
		stmt, err := Parse(`SELECT level, AVG(price) AS p FROM logs GROUP BY level HAVING p > 1.5`)
		require.NoError(t, err)
		assert.Equal(t, []Condition{{Label: "p", Op: ">", Value: 1.5}}, stmt.Having)
	})
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "syntax error",
			sql:  `SELECT FROM`,
			want: "syntax error",
		},
		{
			name: "not a select",
			sql:  `INSERT INTO logs (a) VALUES (1)`,
			want: "only SELECT statements are supported",
		},
		{
			name: "select distinct",
			sql:  `SELECT DISTINCT level FROM logs`,
			want: "unsupported construct: SELECT DISTINCT",
		},
		{
			name: "join",
			sql:  `SELECT a FROM logs JOIN other ON logs.a = other.a`,
			want: "unsupported construct",
		},
		{
			name: "subquery",
			sql:  `SELECT a FROM (SELECT a FROM logs) x`,
			want: "unsupported construct",
		},
		{
			name: "multiple group by keys",
			sql:  `SELECT COUNT(*) FROM logs GROUP BY level, ok`,
			want: "unsupported construct: GROUP BY with multiple keys",
		},
		{
			name: "unsupported group by function",
			sql:  `SELECT COUNT(*) FROM logs GROUP BY UPPER(level)`,
			want: "unsupported construct",
		},
		{
			name: "unsupported select function",
			sql:  `SELECT UPPER(level) FROM logs`,
			want: "unsupported construct",
		},
		{
			name: "star with aggregate",
			sql:  `SELECT *, COUNT(*) FROM logs`,
			want: "unsupported construct: SELECT * with aggregates",
		},
		{
			name: "column not grouped",
			sql:  `SELECT level, COUNT(*) FROM logs`,
			want: "column [level] must appear in GROUP BY or be an aggregate",
		},
		{
			name: "having without aggregation",
			sql:  `SELECT level FROM logs HAVING COUNT(*) > 1`,
			want: "HAVING requires an aggregated query",
		},
		{
			name: "having on unselected aggregate",
			sql:  `SELECT level, COUNT(*) FROM logs GROUP BY level HAVING AVG(price) > 1`,
			want: "HAVING [AVG(price)] must reference a selected aggregate",
		},
		{
			name: "order by unselected column when aggregated",
			sql:  `SELECT level, COUNT(*) FROM logs GROUP BY level ORDER BY price`,
			want: "ORDER BY [price] must reference a selected column",
		},
		{
			name: "order by expression",
			sql:  `SELECT level FROM logs ORDER BY UPPER(level)`,
			want: "unsupported construct: ORDER BY",
		},
		{
			name: "with rollup",
			sql:  `SELECT COUNT(*) FROM logs GROUP BY level WITH ROLLUP`,
			want: "unsupported construct: WITH ROLLUP",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.sql)
			require.Error(t, err)
			var sqlErr *Error
			assert.ErrorAs(t, err, &sqlErr)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestTranslateErrors(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "unsupported where expression",
			sql:  `SELECT * FROM logs WHERE count + 1 = 2`,
			want: "unsupported WHERE expression",
		},
		{
			name: "unsupported where value",
			sql:  `SELECT * FROM logs WHERE count = price`,
			want: "unsupported WHERE expression",
		},
		{
			name: "unsupported where function",
			sql:  `SELECT * FROM logs WHERE ABS(count)`,
			want: "unsupported function [ABS]",
		},
		{
			name: "match arity",
			sql:  `SELECT * FROM logs WHERE MATCH(message)`,
			want: "MATCH requires (field, 'text')",
		},
		{
			name: "query arity",
			sql:  `SELECT * FROM logs WHERE QUERY('a', 'b')`,
			want: "QUERY requires ('lucene query')",
		},
		{
			name: "regexp operator",
			sql:  `SELECT * FROM logs WHERE level REGEXP 'err.*'`,
			want: "unsupported operator",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, err := Parse(tt.sql)
			require.NoError(t, err)
			_, err = BuildDSL(stmt, testFields, 0)
			require.Error(t, err)
			var sqlErr *Error
			assert.ErrorAs(t, err, &sqlErr)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestBuildQuery(t *testing.T) {
	dsl := translateDSL(t, `SELECT level FROM logs WHERE count > 1 ORDER BY level DESC LIMIT 3`)
	query, err := BuildQuery(dsl)
	require.NoError(t, err)
	assert.Equal(t, 3, query.Size)
	assert.True(t, query.TrackTotalHits)
	assert.Equal(t, []interface{}{"level"}, query.Source)
	assert.NotNil(t, query.Query)
}

func TestPreprocess(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "double quotes become identifiers",
			sql:  `SELECT "a b" FROM "logs-*"`,
			want: "SELECT `a b` FROM `logs-*`",
		},
		{
			name: "single quoted strings are kept",
			sql:  `SELECT a FROM t WHERE a = 'x "y" z'`,
			want: `SELECT a FROM t WHERE a = 'x "y" z'`,
		},
		{
			name: "match is renamed",
			sql:  `SELECT a FROM t WHERE MATCH (a, 'x')`,
			want: `SELECT a FROM t WHERE zinc_match (a, 'x')`,
		},
		{
			name: "match inside a string is kept",
			sql:  `SELECT a FROM t WHERE a = 'match(b)'`,
			want: `SELECT a FROM t WHERE a = 'match(b)'`,
		},
		{
			name: "match column is kept",
			sql:  `SELECT match FROM t`,
			want: `SELECT match FROM t`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, preprocess(tt.sql))
		})
	}
}
