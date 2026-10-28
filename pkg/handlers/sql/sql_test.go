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
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta"
	zincsql "github.com/vcaesar/zedsearch/pkg/sql"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
	"github.com/vcaesar/zedsearch/test/utils"
)

var testDocs = []map[string]interface{}{
	{"level": "error", "message": "disk failure on node one", "count": 3},
	{"level": "error", "message": "disk failure on node two", "count": 5},
	{"level": "warn", "message": "memory pressure", "count": 1},
	{"level": "info", "message": "startup complete", "count": 10},
}

// prepareIndex creates an index with the fixture mappings and documents, and
// deletes it when the test ends.
func prepareIndex(t *testing.T, name string, docs []map[string]interface{}) {
	t.Helper()

	index, err := core.NewIndex(name, "disk", 2)
	require.NoError(t, err)
	require.NoError(t, core.StoreIndex(index))
	t.Cleanup(func() {
		assert.NoError(t, core.DeleteIndex(name))
	})

	mappings := meta.NewMappings()
	mappings.SetProperty("level", meta.NewProperty("keyword"))
	mappings.SetProperty("message", meta.NewProperty("text"))
	mappings.SetProperty("count", meta.NewProperty("numeric"))
	require.NoError(t, index.SetMappings(mappings))

	for i, doc := range docs {
		require.NoError(t, index.CreateDocument(strconv.Itoa(i), doc, false))
	}
	require.Eventually(t, func() bool {
		resp, err := index.Search(&meta.ZincQuery{Size: 100})
		return err == nil && resp.Hits.Total.Value == len(docs)
	}, 30*time.Second, 50*time.Millisecond, "documents not indexed")
}

func callSearch(t *testing.T, body string, params map[string]string) (int, string) {
	t.Helper()
	c, w := utils.NewGinContext()
	utils.SetGinRequestData(c, body)
	utils.SetGinRequestURL(c, "/es/_sql", params)
	Search(c)
	return w.Code, w.Body.String()
}

func decodeResponse(t *testing.T, body string) *zincsql.Response {
	t.Helper()
	resp := new(zincsql.Response)
	require.NoError(t, json.Unmarshal([]byte(body), resp))
	return resp
}

func TestSearch(t *testing.T) {
	indexName := "TestSearch.index_1"
	prepareIndex(t, indexName, testDocs)
	from := ` FROM "` + indexName + `"`

	tests := []struct {
		name    string
		sql     string
		columns []zincsql.ResultColumn
		rows    [][]interface{}
	}{
		{
			name: "select where order limit",
			sql:  `SELECT level, count` + from + ` WHERE level = 'error' ORDER BY count DESC LIMIT 1`,
			columns: []zincsql.ResultColumn{
				{Name: "level", Type: "keyword"},
				{Name: "count", Type: "double"},
			},
			rows: [][]interface{}{{"error", 5.0}},
		},
		{
			name: "select where offset",
			sql:  `SELECT count` + from + ` WHERE level = 'error' ORDER BY count ASC LIMIT 1 OFFSET 1`,
			columns: []zincsql.ResultColumn{
				{Name: "count", Type: "double"},
			},
			rows: [][]interface{}{{5.0}},
		},
		{
			name: "count star",
			sql:  `SELECT COUNT(*)` + from,
			columns: []zincsql.ResultColumn{
				{Name: "COUNT(*)", Type: "long"},
			},
			rows: [][]interface{}{{4.0}},
		},
		{
			name: "count star with where",
			sql:  `SELECT COUNT(*) AS total` + from + ` WHERE count >= 5`,
			columns: []zincsql.ResultColumn{
				{Name: "total", Type: "long"},
			},
			rows: [][]interface{}{{2.0}},
		},
		{
			name: "metrics without group by",
			sql:  `SELECT MIN(count), MAX(count), SUM(count)` + from,
			columns: []zincsql.ResultColumn{
				{Name: "MIN(count)", Type: "double"},
				{Name: "MAX(count)", Type: "double"},
				{Name: "SUM(count)", Type: "double"},
			},
			rows: [][]interface{}{{1.0, 10.0, 19.0}},
		},
		{
			name: "group by terms with avg",
			sql:  `SELECT level, COUNT(*), AVG(count) AS avg_count` + from + ` GROUP BY level ORDER BY COUNT(*) DESC, level ASC`,
			columns: []zincsql.ResultColumn{
				{Name: "level", Type: "keyword"},
				{Name: "COUNT(*)", Type: "long"},
				{Name: "avg_count", Type: "double"},
			},
			rows: [][]interface{}{
				{"error", 2.0, 4.0},
				{"info", 1.0, 10.0},
				{"warn", 1.0, 1.0},
			},
		},
		{
			name: "group by with having",
			sql:  `SELECT level, COUNT(*)` + from + ` GROUP BY level HAVING COUNT(*) > 1`,
			columns: []zincsql.ResultColumn{
				{Name: "level", Type: "keyword"},
				{Name: "COUNT(*)", Type: "long"},
			},
			rows: [][]interface{}{{"error", 2.0}},
		},
		{
			name: "group by with limit",
			sql:  `SELECT level, COUNT(*)` + from + ` GROUP BY level ORDER BY COUNT(*) DESC LIMIT 1`,
			columns: []zincsql.ResultColumn{
				{Name: "level", Type: "keyword"},
				{Name: "COUNT(*)", Type: "long"},
			},
			rows: [][]interface{}{{"error", 2.0}},
		},
		{
			name: "count distinct",
			sql:  `SELECT COUNT(DISTINCT level)` + from,
			columns: []zincsql.ResultColumn{
				{Name: "COUNT(DISTINCT level)", Type: "long"},
			},
			rows: [][]interface{}{{3.0}},
		},
		{
			name: "match function",
			sql:  `SELECT COUNT(*)` + from + ` WHERE MATCH(message, 'disk')`,
			columns: []zincsql.ResultColumn{
				{Name: "COUNT(*)", Type: "long"},
			},
			rows: [][]interface{}{{2.0}},
		},
		{
			name: "query function",
			sql:  `SELECT COUNT(*)` + from + ` WHERE QUERY('memory')`,
			columns: []zincsql.ResultColumn{
				{Name: "COUNT(*)", Type: "long"},
			},
			rows: [][]interface{}{{1.0}},
		},
		{
			name: "in and between",
			sql:  `SELECT COUNT(*)` + from + ` WHERE level IN ('error', 'warn') AND count BETWEEN 1 AND 3`,
			columns: []zincsql.ResultColumn{
				{Name: "COUNT(*)", Type: "long"},
			},
			rows: [][]interface{}{{2.0}},
		},
		{
			name: "like",
			sql:  `SELECT COUNT(*)` + from + ` WHERE level LIKE 'err%'`,
			columns: []zincsql.ResultColumn{
				{Name: "COUNT(*)", Type: "long"},
			},
			rows: [][]interface{}{{2.0}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := callSearch(t, `{"query":`+strconv.Quote(tt.sql)+`}`, nil)
			require.Equal(t, http.StatusOK, code, body)
			resp := decodeResponse(t, body)
			assert.Equal(t, tt.columns, resp.Columns)
			assert.Equal(t, tt.rows, resp.Rows)
		})
	}
}

func TestSearchSelectStar(t *testing.T) {
	indexName := "TestSearchSelectStar.index_1"
	prepareIndex(t, indexName, testDocs[:1])

	code, body := callSearch(t, `{"query":"SELECT * FROM \"`+indexName+`\" WHERE level = 'error'"}`, nil)
	require.Equal(t, http.StatusOK, code, body)

	resp := decodeResponse(t, body)
	names := make([]string, 0, len(resp.Columns))
	for _, column := range resp.Columns {
		names = append(names, column.Name)
	}
	assert.Subset(t, names, []string{"level", "message", "count"})
	require.Len(t, resp.Rows, 1)
	assert.Len(t, resp.Rows[0], len(resp.Columns))
}

func TestSearchDateHistogram(t *testing.T) {
	indexName := "TestSearchDateHistogram.index_1"
	prepareIndex(t, indexName, testDocs)

	sql := "SELECT DATE_HISTOGRAM(\"@timestamp\", '1d'), COUNT(*) FROM \"" + indexName +
		"\" GROUP BY DATE_HISTOGRAM(\"@timestamp\", '1d')"
	code, body := callSearch(t, `{"query":`+strconv.Quote(sql)+`}`, nil)
	require.Equal(t, http.StatusOK, code, body)

	resp := decodeResponse(t, body)
	require.Equal(t, []zincsql.ResultColumn{
		{Name: "DATE_HISTOGRAM(@timestamp, '1d')", Type: "datetime"},
		{Name: "COUNT(*)", Type: "long"},
	}, resp.Columns)
	require.Len(t, resp.Rows, 1)

	bucket, ok := resp.Rows[0][0].(string)
	require.True(t, ok, "bucket key is %T", resp.Rows[0][0])
	_, err := time.Parse(time.RFC3339, bucket)
	assert.NoError(t, err)
	assert.Equal(t, 4.0, resp.Rows[0][1])
}

func TestSearchMultipleIndexes(t *testing.T) {
	prepareIndex(t, "TestSearchMultipleIndexes.index_1", testDocs[:2])
	prepareIndex(t, "TestSearchMultipleIndexes.index_2", testDocs[2:])

	tests := []struct {
		name string
		from string
	}{
		{name: "index list", from: `"TestSearchMultipleIndexes.index_1", "TestSearchMultipleIndexes.index_2"`},
		{name: "wildcard", from: `"TestSearchMultipleIndexes.index_*"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := callSearch(t, `{"query":`+strconv.Quote(`SELECT COUNT(*) FROM `+tt.from)+`}`, nil)
			require.Equal(t, http.StatusOK, code, body)
			assert.Equal(t, [][]interface{}{{4.0}}, decodeResponse(t, body).Rows)
		})
	}
}

func TestSearchFetchSize(t *testing.T) {
	indexName := "TestSearchFetchSize.index_1"
	prepareIndex(t, indexName, testDocs)

	body := `{"query":"SELECT level FROM \"` + indexName + `\"","fetch_size":2}`
	code, out := callSearch(t, body, nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Len(t, decodeResponse(t, out).Rows, 2)
}

func TestSearchFormats(t *testing.T) {
	indexName := "TestSearchFormats.index_1"
	prepareIndex(t, indexName, testDocs[:1])
	query := `SELECT level, count FROM \"` + indexName + `\" WHERE level = 'error'`

	tests := []struct {
		name        string
		body        string
		params      map[string]string
		accept      string
		contentType string
		want        string
	}{
		{
			name:        "json by default",
			body:        `{"query":"` + query + `"}`,
			contentType: "application/json; charset=utf-8",
			want:        `{"columns":[{"name":"level","type":"keyword"},{"name":"count","type":"double"}],"rows":[["error",3]]}`,
		},
		{
			name:        "txt by query param",
			body:        `{"query":"` + query + `"}`,
			params:      map[string]string{"format": "txt"},
			contentType: "text/plain; charset=utf-8",
			want:        "level|count\n-----+-----\nerror|3    \n",
		},
		{
			name:        "csv by body",
			body:        `{"query":"` + query + `","format":"csv"}`,
			contentType: "text/csv; charset=utf-8",
			want:        "level,count\nerror,3\n",
		},
		{
			name:        "tsv by accept header",
			body:        `{"query":"` + query + `"}`,
			accept:      "text/tab-separated-values",
			contentType: "text/tab-separated-values; charset=utf-8",
			want:        "level\tcount\nerror\t3\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := utils.NewGinContext()
			utils.SetGinRequestData(c, tt.body)
			utils.SetGinRequestURL(c, "/es/_sql", tt.params)
			if tt.accept != "" {
				c.Request.Header.Set("Accept", tt.accept)
			}
			Search(c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, tt.contentType, w.Header().Get("Content-Type"))
			assert.Equal(t, tt.want, w.Body.String())
		})
	}
}

func TestTranslate(t *testing.T) {
	indexName := "TestTranslate.index_1"
	prepareIndex(t, indexName, testDocs[:1])

	sql := `SELECT level FROM "` + indexName + `" WHERE message = 'disk' AND count > 1 LIMIT 2`
	c, w := utils.NewGinContext()
	utils.SetGinRequestData(c, `{"query":`+strconv.Quote(sql)+`}`)
	Translate(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var dsl map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &dsl))
	assert.Equal(t, float64(2), dsl["size"])
	assert.Equal(t, []interface{}{"level"}, dsl["_source"])
	assert.Equal(t, map[string]interface{}{
		"bool": map[string]interface{}{
			"must": []interface{}{
				map[string]interface{}{"match": map[string]interface{}{
					"message": map[string]interface{}{"query": "disk"},
				}},
				map[string]interface{}{"range": map[string]interface{}{
					"count": map[string]interface{}{"gt": float64(1)},
				}},
			},
		},
	}, dsl["query"])
}

func TestSearchErrors(t *testing.T) {
	indexName := "TestSearchErrors.index_1"
	prepareIndex(t, indexName, testDocs[:1])

	tests := []struct {
		name   string
		body   string
		params map[string]string
		want   string
	}{
		{
			name: "invalid json",
			body: `{"query": }`,
			want: "invalid character",
		},
		{
			name: "missing query",
			body: `{}`,
			want: "query is required",
		},
		{
			name: "syntax error",
			body: `{"query":"SELECT FROM"}`,
			want: "syntax error",
		},
		{
			name: "unsupported construct",
			body: `{"query":"SELECT DISTINCT level FROM \"` + indexName + `\""}`,
			want: "unsupported construct: SELECT DISTINCT",
		},
		{
			name: "unknown index",
			body: `{"query":"SELECT * FROM nonexistent_index_for_sql"}`,
			want: "does not exists",
		},
		{
			name:   "unknown format",
			body:   `{"query":"SELECT * FROM \"` + indexName + `\""}`,
			params: map[string]string{"format": "yaml"},
			want:   "unsupported format [yaml]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := callSearch(t, tt.body, tt.params)
			assert.Equal(t, http.StatusBadRequest, code)
			assert.Contains(t, body, tt.want)
		})
	}
}

func TestTranslateErrors(t *testing.T) {
	c, w := utils.NewGinContext()
	utils.SetGinRequestData(c, `{"query":"SELECT * FROM logs WHERE count + 1 = 2"}`)
	Translate(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "unsupported WHERE expression")
}
