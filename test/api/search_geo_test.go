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

	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

func TestSearchGeo(t *testing.T) {
	index := "TestSearchGeo.index_1"
	request("DELETE", "/api/index/"+index, nil) // drop leftovers from a previous run

	t.Run("create index with geo_point mapping", func(t *testing.T) {
		body := bytes.NewBufferString(`{
			"name": "` + index + `",
			"storage_type": "disk",
			"mappings": {"properties": {
				"name": {"type": "keyword"},
				"location": {"type": "geo_point"}
			}}
		}`)
		resp := request("PUT", "/api/index", body)
		assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	})

	docs := map[string]string{
		"sf":      `{"name": "sf", "location": {"lat": 37.7749, "lon": -122.4194}}`,
		"oakland": `{"name": "oakland", "location": [-122.2712, 37.8044]}`,
		"la":      `{"name": "la", "location": "34.0522,-118.2437"}`,
		"nyc":     `{"name": "nyc", "location": {"type": "Point", "coordinates": [-74.0060, 40.7128]}}`,
	}
	t.Run("index documents", func(t *testing.T) {
		for id, doc := range docs {
			resp := request("PUT", "/api/"+index+"/_doc/"+id, bytes.NewBufferString(doc))
			assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		}
		for id := range docs {
			waitForDocument(t, index, id)
		}
	})

	t.Run("reject invalid geo_point", func(t *testing.T) {
		resp := request("PUT", "/api/"+index+"/_doc/bad", bytes.NewBufferString(`{"location": "not a point"}`))
		// document validation errors are reported as 500 by the handler, same as bad dates
		assert.Equal(t, http.StatusInternalServerError, resp.Code)
		assert.Contains(t, resp.Body.String(), "geo_point")
	})

	hits := func(t *testing.T, query string) []string {
		t.Helper()
		resp := request("POST", "/es/"+index+"/_search", bytes.NewBufferString(query))
		assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		data := new(meta.SearchResponse)
		assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), data))
		ids := make([]string, 0, len(data.Hits.Hits))
		for _, h := range data.Hits.Hits {
			ids = append(ids, h.ID)
		}
		return ids
	}

	t.Run("geo_distance", func(t *testing.T) {
		got := hits(t, `{"query": {"geo_distance": {"distance": "20km", "location": {"lat": 37.7749, "lon": -122.4194}}}}`)
		assert.ElementsMatch(t, []string{"sf", "oakland"}, got)
	})

	t.Run("geo_bounding_box", func(t *testing.T) {
		got := hits(t, `{"query": {"geo_bounding_box": {"location": {
			"top_left": {"lat": 42, "lon": -125}, "bottom_right": {"lat": 32, "lon": -114}}}}}`)
		assert.ElementsMatch(t, []string{"sf", "oakland", "la"}, got)
	})

	t.Run("geo_polygon", func(t *testing.T) {
		got := hits(t, `{"query": {"bool": {"filter": {"geo_polygon": {"location": {
			"points": [{"lat": 45, "lon": -80}, {"lat": 45, "lon": -70}, {"lat": 35, "lon": -70}, {"lat": 35, "lon": -80}]}}}}}}`)
		assert.ElementsMatch(t, []string{"nyc"}, got)
	})

	t.Run("geo query on non geo field", func(t *testing.T) {
		resp := request("POST", "/es/"+index+"/_search", bytes.NewBufferString(
			`{"query": {"geo_distance": {"distance": "20km", "name": "37,-122"}}}`))
		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("cleanup", func(t *testing.T) {
		resp := request("DELETE", "/api/index/"+index, nil)
		assert.Equal(t, http.StatusOK, resp.Code)
	})
}
