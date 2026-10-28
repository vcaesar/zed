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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/routes"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

var (
	sqlEngine *zhttp.Engine
	sqlOnce   sync.Once
)

// sqlServer is the shared engine plus the SQL endpoints, which live in their
// own route set.
func sqlServer() *zhttp.Engine {
	sqlOnce.Do(func() {
		sqlEngine = zhttp.New()
		routes.SetRoutes(sqlEngine)
		routes.SetSQLRoutes(sqlEngine)
	})
	return sqlEngine
}

func sqlRequest(method, api string, body io.Reader) *httptest.ResponseRecorder {
	req, _ := http.NewRequest(method, api, body)
	req.SetBasicAuth(username, password)
	w := httptest.NewRecorder()
	sqlServer().ServeHTTP(w, req)
	return w
}

func sqlQuery(t *testing.T, api, body string) *httptest.ResponseRecorder {
	t.Helper()
	return sqlRequest("POST", api, bytes.NewBufferString(body))
}

const sqlIndexName = "sql_api_test"

func prepareSQLIndex(t *testing.T) {
	t.Helper()

	sqlRequest("DELETE", "/api/index/"+sqlIndexName, nil) // drop leftovers from a previous run
	resp := sqlRequest("PUT", "/api/index", bytes.NewBufferString(`{
		"name":"`+sqlIndexName+`",
		"storage_type":"disk",
		"mappings":{"properties":{
			"level":{"type":"keyword","index":true,"sortable":true,"aggregatable":true},
			"message":{"type":"text","index":true},
			"count":{"type":"numeric","index":true,"sortable":true,"aggregatable":true}
		}}}`))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	docs := []string{
		`{"level":"error","message":"disk failure on node one","count":3}`,
		`{"level":"error","message":"disk failure on node two","count":5}`,
		`{"level":"warn","message":"memory pressure","count":1}`,
	}
	for _, doc := range docs {
		resp := sqlRequest("PUT", "/api/"+sqlIndexName+"/_doc", bytes.NewBufferString(doc))
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

		created := new(meta.HTTPResponseID)
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), created))
		waitForDocument(t, sqlIndexName, created.ID)
	}
}

func TestSQL(t *testing.T) {
	prepareSQLIndex(t)
	from := ` FROM \"` + sqlIndexName + `\"`

	t.Run("POST /es/_sql", func(t *testing.T) {
		t.Run("select rows as json", func(t *testing.T) {
			resp := sqlQuery(t, "/es/_sql",
				`{"query":"SELECT level, count`+from+` WHERE level = 'error' ORDER BY count DESC"}`)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			assert.Equal(t, "Elasticsearch", resp.Header().Get("X-elastic-product"))

			data := struct {
				Columns []struct {
					Name string `json:"name"`
					Type string `json:"type"`
				} `json:"columns"`
				Rows [][]interface{} `json:"rows"`
			}{}
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &data))
			require.Len(t, data.Columns, 2)
			assert.Equal(t, "level", data.Columns[0].Name)
			assert.Equal(t, "keyword", data.Columns[0].Type)
			assert.Equal(t, "count", data.Columns[1].Name)
			assert.Equal(t, "double", data.Columns[1].Type)
			assert.Equal(t, [][]interface{}{{"error", 5.0}, {"error", 3.0}}, data.Rows)
		})

		t.Run("group by as txt", func(t *testing.T) {
			resp := sqlQuery(t, "/es/_sql?format=txt",
				`{"query":"SELECT level, COUNT(*)`+from+` GROUP BY level ORDER BY COUNT(*) DESC"}`)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			assert.Equal(t, "text/plain; charset=utf-8", resp.Header().Get("Content-Type"))
			assert.Equal(t,
				"level|COUNT(*)\n"+
					"-----+--------\n"+
					"error|2       \n"+
					"warn |1       \n",
				resp.Body.String())
		})

		t.Run("count as csv", func(t *testing.T) {
			resp := sqlQuery(t, "/es/_sql",
				`{"query":"SELECT COUNT(*)`+from+`","format":"csv"}`)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			assert.Equal(t, "text/csv; charset=utf-8", resp.Header().Get("Content-Type"))
			assert.Equal(t, "COUNT(*)\n3\n", resp.Body.String())
		})

		t.Run("fetch size caps rows", func(t *testing.T) {
			resp := sqlQuery(t, "/es/_sql",
				`{"query":"SELECT level`+from+`","fetch_size":1,"format":"csv"}`)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			assert.Equal(t, 2, len(strings.Split(strings.TrimSuffix(resp.Body.String(), "\n"), "\n")))
		})

		t.Run("unsupported construct", func(t *testing.T) {
			resp := sqlQuery(t, "/es/_sql", `{"query":"SELECT DISTINCT level`+from+`"}`)
			assert.Equal(t, http.StatusBadRequest, resp.Code)
			assert.Contains(t, resp.Body.String(), "unsupported construct: SELECT DISTINCT")
		})

		t.Run("parse error", func(t *testing.T) {
			resp := sqlQuery(t, "/es/_sql", `{"query":"SELECT FROM"}`)
			assert.Equal(t, http.StatusBadRequest, resp.Code)
			assert.Contains(t, resp.Body.String(), "syntax error")
		})
	})

	t.Run("POST /es/_sql/translate", func(t *testing.T) {
		resp := sqlQuery(t, "/es/_sql/translate",
			`{"query":"SELECT level`+from+` WHERE count > 1 LIMIT 5"}`)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

		dsl := make(map[string]interface{})
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dsl))
		assert.Equal(t, float64(5), dsl["size"])
		assert.Equal(t, float64(0), dsl["from"])
		assert.Equal(t, true, dsl["track_total_hits"])
		assert.Equal(t, []interface{}{"level"}, dsl["_source"])
		assert.Equal(t, map[string]interface{}{
			"range": map[string]interface{}{
				"count": map[string]interface{}{"gt": float64(1)},
			},
		}, dsl["query"])
	})

	t.Run("POST /api/_sql", func(t *testing.T) {
		resp := sqlQuery(t, "/api/_sql", `{"query":"SELECT COUNT(*)`+from+` WHERE MATCH(message, 'disk')"}`)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		assert.Contains(t, resp.Body.String(), `"rows":[[2]]`)
	})

	t.Run("cleanup", func(t *testing.T) {
		resp := sqlRequest("DELETE", "/api/index/"+sqlIndexName, nil)
		assert.Equal(t, http.StatusOK, resp.Code)
	})
}
