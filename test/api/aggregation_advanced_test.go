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
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

// aggregationAdvancedDocs is the shared fixture: four sales per city.
var aggregationAdvancedDocs = map[string]string{
	"1": `{"city": "sf", "name": "one", "price": 100}`,
	"2": `{"city": "sf", "name": "two", "price": 200}`,
	"3": `{"city": "la", "name": "three", "price": 50}`,
	"4": `{"city": "nyc", "name": "four", "price": 400}`,
}

func TestAggregationAdvanced(t *testing.T) {
	index := "TestAggregationAdvanced.index_1"
	request("DELETE", "/api/index/"+index, nil) // drop leftovers from previous run

	t.Run("create index", func(t *testing.T) {
		body := bytes.NewBufferString(`{
			"name": "` + index + `",
			"storage_type": "disk",
			"mappings": {"properties": {
				"city": {"type": "keyword"},
				"name": {"type": "text"},
				"price": {"type": "numeric"}
			}}
		}`)
		resp := request("PUT", "/api/index", body)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	})

	t.Run("index documents", func(t *testing.T) {
		for id, doc := range aggregationAdvancedDocs {
			resp := request("PUT", "/api/"+index+"/_doc/"+id, bytes.NewBufferString(doc))
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		}
		for id := range aggregationAdvancedDocs {
			waitForDocument(t, index, id)
		}
	})

	// aggs runs a search and returns the aggregations as a generic tree.
	aggs := func(t *testing.T, query string) map[string]interface{} {
		t.Helper()
		resp := request("POST", "/es/"+index+"/_search", bytes.NewBufferString(query))
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		data := make(map[string]interface{})
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &data))
		rv, ok := data["aggregations"].(map[string]interface{})
		require.True(t, ok, resp.Body.String())
		return rv
	}

	bucketsOf := func(t *testing.T, agg interface{}) []interface{} {
		t.Helper()
		m, ok := agg.(map[string]interface{})
		require.True(t, ok)
		buckets, ok := m["buckets"].([]interface{})
		require.True(t, ok)
		return buckets
	}

	bucketKeys := func(t *testing.T, agg interface{}) []string {
		t.Helper()
		rv := []string{}
		for _, b := range bucketsOf(t, agg) {
			rv = append(rv, b.(map[string]interface{})["key"].(string))
		}
		return rv
	}

	t.Run("filter", func(t *testing.T) {
		got := aggs(t, `{"size": 0, "aggs": {"bay": {
			"filter": {"term": {"city": {"value": "sf"}}},
			"aggs": {"total": {"sum": {"field": "price"}}}
		}}}`)
		bay := got["bay"].(map[string]interface{})
		assert.Equal(t, float64(2), bay["doc_count"])
		assert.Equal(t, float64(300), bay["total"].(map[string]interface{})["value"])
	})

	t.Run("filters", func(t *testing.T) {
		got := aggs(t, `{"size": 0, "aggs": {"by_city": {"filters": {
			"filters": {
				"bay": {"term": {"city": {"value": "sf"}}},
				"socal": {"terms": {"city": ["la"]}}
			},
			"other_bucket_key": "other"
		}}}}`)
		buckets := got["by_city"].(map[string]interface{})["buckets"].(map[string]interface{})
		assert.Equal(t, float64(2), buckets["bay"].(map[string]interface{})["doc_count"])
		assert.Equal(t, float64(1), buckets["socal"].(map[string]interface{})["doc_count"])
		assert.Equal(t, float64(1), buckets["other"].(map[string]interface{})["doc_count"])
	})

	t.Run("multi_terms", func(t *testing.T) {
		got := aggs(t, `{"size": 0, "aggs": {"combo": {"multi_terms": {
			"terms": [{"field": "city"}, {"field": "price"}],
			"size": 10,
			"order": {"_key": "asc"}
		}}}}`)
		keys := []string{}
		for _, b := range bucketsOf(t, got["combo"]) {
			keys = append(keys, b.(map[string]interface{})["key_as_string"].(string))
		}
		assert.Equal(t, []string{"la|50", "nyc|400", "sf|100", "sf|200"}, keys)
	})

	t.Run("top_hits", func(t *testing.T) {
		got := aggs(t, `{"size": 0, "query": {"term": {"city": {"value": "sf"}}}, "aggs": {"best": {
			"top_hits": {"size": 2, "_source": ["city", "price"]}
		}}}`)
		hits := got["best"].(map[string]interface{})["hits"].(map[string]interface{})
		assert.Equal(t, float64(2), hits["total"].(map[string]interface{})["value"])

		list := hits["hits"].([]interface{})
		require.Len(t, list, 2)
		ids := []string{}
		for _, h := range list {
			hit := h.(map[string]interface{})
			ids = append(ids, hit["_id"].(string))
			assert.Contains(t, hit["_source"], "city")
			assert.NotContains(t, hit["_source"], "name")
		}
		assert.ElementsMatch(t, []string{"1", "2"}, ids)
	})

	t.Run("percentile_ranks", func(t *testing.T) {
		got := aggs(t, `{"size": 0, "aggs": {"ranks": {
			"percentile_ranks": {"field": "price", "values": [100, 400]}
		}}}`)
		values := got["ranks"].(map[string]interface{})["values"].(map[string]interface{})
		assert.Contains(t, values, "100.0")
		assert.Equal(t, float64(100), values["400.0"])
	})

	// Without nested documents in the index the nested bucket is empty and
	// reverse_nested holds every root document.
	t.Run("nested and reverse_nested", func(t *testing.T) {
		got := aggs(t, `{"size": 0, "aggs": {
			"comments": {"nested": {"path": "comments"}},
			"roots": {"reverse_nested": {}}
		}}`)
		assert.Equal(t, float64(0), got["comments"].(map[string]interface{})["doc_count"])
		assert.Equal(t, float64(4), got["roots"].(map[string]interface{})["doc_count"])
	})

	t.Run("terms order by key", func(t *testing.T) {
		got := aggs(t, `{"size": 0, "aggs": {"cities": {
			"terms": {"field": "city", "size": 10, "order": {"_key": "asc"}}
		}}}`)
		assert.Equal(t, []string{"la", "nyc", "sf"}, bucketKeys(t, got["cities"]))
	})

	t.Run("global is not implemented", func(t *testing.T) {
		resp := request("POST", "/es/"+index+"/_search",
			bytes.NewBufferString(`{"size": 0, "aggs": {"all": {"global": {}}}}`))
		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "match_all")
	})

	t.Run("bucket_script under terms", func(t *testing.T) {
		got := aggs(t, `{"size": 0, "aggs": {"cities": {
			"terms": {"field": "city", "size": 10, "order": {"_key": "asc"}},
			"aggs": {
				"total": {"sum": {"field": "price"}},
				"avg_price": {"bucket_script": {
					"buckets_path": {"t": "total", "c": "_count"},
					"script": "params.t / params.c"
				}}
			}
		}}}`)
		want := map[string]float64{"la": 50, "nyc": 400, "sf": 150}
		for _, b := range bucketsOf(t, got["cities"]) {
			bucket := b.(map[string]interface{})
			key := bucket["key"].(string)
			assert.Equal(t, want[key], bucket["avg_price"].(map[string]interface{})["value"], key)
		}
	})

	t.Run("bucket_selector and bucket_sort under terms", func(t *testing.T) {
		got := aggs(t, `{"size": 0, "aggs": {"cities": {
			"terms": {"field": "city", "size": 10},
			"aggs": {
				"total": {"sum": {"field": "price"}},
				"big": {"bucket_selector": {
					"buckets_path": {"total": "total"},
					"script": "params.total > 100"
				}},
				"ranked": {"bucket_sort": {"sort": [{"total": {"order": "desc"}}], "size": 2}}
			}
		}}}`)
		assert.Equal(t, []string{"nyc", "sf"}, bucketKeys(t, got["cities"]))
	})

	t.Run("avg_bucket over terms", func(t *testing.T) {
		got := aggs(t, `{"size": 0, "aggs": {
			"cities": {
				"terms": {"field": "city", "size": 10},
				"aggs": {"total": {"sum": {"field": "price"}}}
			},
			"avg_city_sales": {"avg_bucket": {"buckets_path": "cities>total"}}
		}}`)
		// (300 + 50 + 400) / 3
		assert.InDelta(t, 250, got["avg_city_sales"].(map[string]interface{})["value"], 1e-9)
	})

	t.Run("cleanup", func(t *testing.T) {
		resp := request("DELETE", "/api/index/"+index, nil)
		assert.Equal(t, http.StatusOK, resp.Code)
	})
}
