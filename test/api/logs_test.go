/* Copyright 2022 Zinc Labs Inc. and Contributors
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
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch/pkg/core"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

func cleanupLogsIndex(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() {
		if _, exists := core.GetIndex(name); exists {
			assert.NoError(t, core.DeleteIndex(name))
		}
	})
}

func assertLogsMetadata(t *testing.T, name string) {
	t.Helper()
	for _, prefix := range []string{"/api/", "/es/"} {
		resp := request("GET", prefix+name+"/_settings", nil)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		var settings map[string]struct {
			Settings struct {
				IndexType string `json:"index_type"`
			} `json:"settings"`
		}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &settings))
		assert.Equal(t, "logs", settings[name].Settings.IndexType)
	}

	resp := request("GET", "/api/"+name+"/_mapping", nil)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var mappings map[string]struct {
		Mappings meta.Mappings `json:"mappings"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &mappings))
	for field, typ := range map[string]string{
		"@timestamp": "date", "message": "text", "log": "text",
		"level": "keyword", "log.level": "keyword", "service.name": "keyword",
		"host.name": "keyword", "trace_id": "keyword", "span_id": "keyword", "tags": "keyword",
	} {
		assert.Equal(t, typ, mappings[name].Mappings.Properties[field].Type, "mapping for %s", field)
	}
}

func searchLogs(t *testing.T, path, query string) meta.SearchResponse {
	t.Helper()
	resp := request("POST", path, strings.NewReader(query))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var result meta.SearchResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	require.Empty(t, result.Error)
	return result
}

func TestLogsIngestionSearch(t *testing.T) {
	for _, route := range []string{"api", "es"} {
		t.Run(route, func(t *testing.T) {
			name := fmt.Sprintf("TestLogsIngestionSearch.%s_%d", route, time.Now().UnixNano())
			cleanupLogsIndex(t, name)
			method, path := "POST", "/api/index"
			if route == "es" {
				method, path = "PUT", "/es/"+name
			}
			resp := request(method, path, strings.NewReader(`{"name":"`+name+`","settings":{"index_type":"logs"}}`))
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			assertLogsMetadata(t, name)

			resp = request("PUT", "/"+route+"/"+name+"/_doc/doc", strings.NewReader(`{
				"@timestamp":"2024-01-01T02:00:03.123456789+02:00", "_timestamp":"2000-01-01T00:00:00Z",
				"message":"request completed", "log":"original raw line", "level":"INFO", "service":{"name":"checkout"}
			}`))
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			resp = request("POST", "/api/"+name+"/_multi", strings.NewReader(
				`{"_id":"multi","_timestamp":1704067201123456,"message":"microsecond event","level":"WARN"}`+"\n"))
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			resp = request("POST", "/api/"+name+"/_bulkv2", strings.NewReader(`{"records":[{
				"_id":"json", "@timestamp":null, "_timestamp":"2024-01-01T02:00:02+02:00", "log":"alias event", "level":"INFO"
			}]}`))
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			resp = request("POST", "/es/_bulk", strings.NewReader(
				`{"index":{"_index":"`+name+`","_id":"bulk"}}`+"\n"+
					`{"@timestamp":1704067200,"@version":"1","message":"Logstash event","host":{"name":"web-1"},"tags":["production"],"level":"ERROR"}`+"\n"))
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

			before := time.Now().UTC()
			resp = request("PUT", "/api/"+name+"/_doc/now", strings.NewReader(`{"message":"ingestion time"}`))
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			after := time.Now().UTC()
			for _, id := range []string{"doc", "multi", "json", "bulk", "now"} {
				waitForDocument(t, name, id)
			}

			all := searchLogs(t, "/es/"+name+"/_search", `{"query":{"match_all":{}},"size":10}`)
			require.Equal(t, 5, all.Hits.Total.Value)
			require.Len(t, all.Hits.Hits, 5)
			wantTimes := map[string]string{
				"doc": "2024-01-01T00:00:03.123456789Z", "multi": "2024-01-01T00:00:01.123456Z",
				"json": "2024-01-01T00:00:02Z", "bulk": "2024-01-01T00:00:00Z",
			}
			for _, hit := range all.Hits.Hits {
				source, ok := hit.Source.(map[string]interface{})
				require.True(t, ok)
				if hit.ID == "now" {
					value, ok := source["@timestamp"].(string)
					require.True(t, ok)
					timestamp, err := time.Parse(time.RFC3339Nano, value)
					require.NoError(t, err)
					assert.False(t, timestamp.Before(before))
					assert.False(t, timestamp.After(after))
					continue
				}
				assert.Equal(t, wantTimes[hit.ID], source["@timestamp"], hit.ID)
				assert.Equal(t, wantTimes[hit.ID], hit.Timestamp.UTC().Format(time.RFC3339Nano), "WAL timestamp for %s", hit.ID)
				switch hit.ID {
				case "doc":
					assert.Equal(t, "request completed", source["message"])
					assert.Equal(t, "original raw line", source["log"])
					assert.Equal(t, "2000-01-01T00:00:00Z", source["_timestamp"])
				case "multi":
					assert.Equal(t, float64(1704067201123456), source["_timestamp"])
					assert.NotContains(t, source, "log")
				case "json":
					assert.Equal(t, "2024-01-01T02:00:02+02:00", source["_timestamp"])
					assert.Equal(t, "alias event", source["log"])
					assert.NotContains(t, source, "message")
				case "bulk":
					assert.Equal(t, "Logstash event", source["message"])
					assert.Equal(t, "1", source["@version"])
				}
			}

			for _, search := range []struct {
				path, query string
			}{
				{"/api/" + name + "/_search", `{"search_type":"alldocuments","query":{"start_time":"2024-01-01T00:00:01Z","end_time":"2024-01-01T00:00:04Z"},"sort_fields":["-@timestamp"],"max_results":10}`},
				{"/es/" + name + "/_search", `{"query":{"range":{"@timestamp":{"gte":"2024-01-01T00:00:01Z","lt":"2024-01-01T00:00:04Z"}}},"sort":[{"@timestamp":"desc"}],"size":10}`},
			} {
				result := searchLogs(t, search.path, search.query)
				assert.Equal(t, 3, result.Hits.Total.Value, search.path)
				ids := make([]string, 0, len(result.Hits.Hits))
				for _, hit := range result.Hits.Hits {
					ids = append(ids, hit.ID)
				}
				assert.Equal(t, []string{"doc", "json", "multi"}, ids, search.path)
			}

			result := searchLogs(t, "/es/"+name+"/_search", `{
				"query":{"range":{"@timestamp":{"gte":"2024-01-01T00:00:00Z","lt":"2024-01-02T00:00:00Z"}}}, "size":0,
				"aggs":{"days":{"date_histogram":{"field":"@timestamp","calendar_interval":"day","format":"epoch_millis"}}}
			}`)
			buckets, ok := result.Aggregations["days"].Buckets.([]interface{})
			require.True(t, ok)
			require.Len(t, buckets, 1)
			bucket, ok := buckets[0].(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, float64(4), bucket["doc_count"])
			assert.Equal(t, float64(1704067200000), bucket["key"])

			resp = request("PUT", "/"+route+"/"+name+"/_doc/invalid", strings.NewReader(`{"@timestamp":"not-a-date","message":"reject me"}`))
			assert.GreaterOrEqual(t, resp.Code, http.StatusBadRequest, resp.Body.String())
			assert.Contains(t, resp.Body.String(), "@timestamp")
			result = searchLogs(t, "/es/"+name+"/_search", `{"query":{"match_all":{}},"size":10}`)
			assert.Equal(t, 5, result.Hits.Total.Value)
		})
	}
}

func TestLogsTemplateBulkCreation(t *testing.T) {
	name := fmt.Sprintf("TestLogsTemplateBulkCreation.%d", time.Now().UnixNano())
	template := name + "_template"
	cleanupLogsIndex(t, name)
	resp := request("PUT", "/es/_index_template/"+template, strings.NewReader(`{
		"index_patterns":["`+name+`*"], "priority":100,
		"template":{"settings":{"index_type":"logs"},"mappings":{"properties":{"application":{"type":"keyword"}}}}
	}`))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	t.Cleanup(func() {
		resp := request("DELETE", "/es/_index_template/"+template, nil)
		assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	})
	_, exists := core.GetIndex(name)
	require.False(t, exists)
	resp = request("POST", "/es/_bulk", strings.NewReader(
		`{"index":{"_index":"`+name+`","_id":"event"}}`+"\n"+
			`{"_timestamp":1704067201123456,"message":"auto-created log","application":"checkout","level":"INFO"}`+"\n"))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	waitForDocument(t, name, "event")
	assertLogsMetadata(t, name)
	result := searchLogs(t, "/es/"+name+"/_search", `{"query":{"bool":{"filter":[{"term":{"application":"checkout"}},{"term":{"level":"INFO"}},{"range":{"@timestamp":{"gte":"2024-01-01T00:00:01Z","lt":"2024-01-01T00:00:02Z"}}}]}},"size":10}`)
	require.Equal(t, 1, result.Hits.Total.Value)
	require.Len(t, result.Hits.Hits, 1)
	source, ok := result.Hits.Hits[0].Source.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "2024-01-01T00:00:01.123456Z", source["@timestamp"])
	assert.Equal(t, float64(1704067201123456), source["_timestamp"])
}

func TestLogsIndexTypeValidation(t *testing.T) {
	for _, route := range []string{"api", "es"} {
		for _, tc := range []struct {
			name, settings string
			status         int
		}{
			{"default", `{}`, http.StatusOK},
			{"empty", `{"index_type":""}`, http.StatusOK},
			{"unknown", `{"index_type":"unknown"}`, http.StatusBadRequest},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				name := fmt.Sprintf("TestLogsIndexTypeValidation.%s_%s_%d", route, tc.name, time.Now().UnixNano())
				cleanupLogsIndex(t, name)
				method, path := "POST", "/api/index"
				if route == "es" {
					method, path = "PUT", "/es/"+name
				}
				resp := request(method, path, strings.NewReader(`{"name":"`+name+`","settings":`+tc.settings+`}`))
				require.Equal(t, tc.status, resp.Code, resp.Body.String())
				if tc.status != http.StatusOK {
					_, exists := core.GetIndex(name)
					assert.False(t, exists)
					return
				}
				resp = request("GET", "/"+route+"/"+name+"/_mapping", nil)
				require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
				var mappings map[string]struct {
					Mappings meta.Mappings `json:"mappings"`
				}
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &mappings))
				assert.NotContains(t, mappings[name].Mappings.Properties, "message")
				assert.NotContains(t, mappings[name].Mappings.Properties, "level")
			})
		}
	}
}
