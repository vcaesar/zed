/* Copyright 2026 AtomAI. and Contributors
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

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

func TestGenerators(t *testing.T) {
	end := time.Date(2026, 10, 28, 10, 30, 0, 0, time.UTC)
	tests := []struct {
		name   string
		gen    func(string, int, time.Time) []map[string]any
		step   time.Duration
		fields []string
	}{
		{"products", products, time.Hour, []string{"name", "category", "tags", "price", "stock", "in_stock", "rating", "embedding", "@timestamp"}},
		{"logs", logs, time.Minute, []string{"level", "service", "host", "message", "status", "duration_ms", "trace_id", "@timestamp"}},
		{"stores", stores, time.Hour, []string{"name", "city", "location", "open", "rating", "@timestamp"}},
		{"forum", forum, 10 * time.Minute, []string{"title", "topic", "author", "votes", "qa", "@timestamp"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := tt.gen("idx", 20, end)
			assert.Len(t, docs, 20)
			assert.Equal(t, docs, tt.gen("idx", 20, end), "must be deterministic")
			for i, doc := range docs {
				for _, f := range tt.fields {
					assert.Contains(t, doc, f)
				}
				ts, err := time.Parse(time.RFC3339, doc["@timestamp"].(string))
				assert.NoError(t, err)
				assert.Equal(t, end.Add(-time.Duration(19-i)*tt.step), ts)
			}
		})
	}

	t.Run("forum join", func(t *testing.T) {
		docs := forum("idx", 8, end)
		for i, doc := range docs {
			if i%4 == 0 {
				assert.Equal(t, "question", doc["qa"])
				assert.NotEmpty(t, doc["comments"])
				continue
			}
			assert.Equal(t, map[string]any{"name": "answer", "parent": docID("idx", i/4*4)}, doc["qa"])
		}
	})
}

func TestRun(t *testing.T) {
	prefix := "TestRun.mock_"
	end := time.Now().UTC().Truncate(time.Minute)
	counts := map[string]int{"products": 5, "logs": 7, "stores": 6, "forum": 9}
	assert.NoError(t, run(prefix, counts, end, false))
	// rerun upserts the same ids instead of duplicating, and returns only once
	// the updated timestamps are searchable
	assert.NoError(t, run(prefix, counts, end.Add(time.Minute), false))
	// reset recreates the indexes from scratch
	assert.NoError(t, run(prefix, counts, end.Add(time.Minute), true))

	search := func(t *testing.T, name, body string) *meta.SearchResponse {
		index, ok := core.ZED_INDEX_LIST.Get(name)
		assert.True(t, ok)
		query := &meta.ZedQuery{Size: 100}
		require.NoError(t, json.Unmarshal([]byte(body), query))
		res, err := index.Search(query)
		require.NoError(t, err)
		return res
	}

	for _, d := range datasets {
		name := prefix + d.name
		t.Run(name, func(t *testing.T) {
			res := search(t, name, `{"query": {"match_all": {}}}`)
			assert.Equal(t, counts[d.name], res.Hits.Total.Value)
			index, _ := core.ZED_INDEX_LIST.Get(name)
			var stamps []string
			for _, doc := range d.gen(name, counts[d.name], end.Add(time.Minute)) {
				stamps = append(stamps, doc["@timestamp"].(string))
			}
			done, err := indexed(index, name, stamps)
			assert.NoError(t, err)
			assert.Equal(t, counts[d.name], done)
		})
	}

	t.Run("features", func(t *testing.T) {
		logsIndex, _ := core.ZED_INDEX_LIST.Get(prefix + "logs")
		assert.Equal(t, meta.IndexTypeLogs, logsIndex.GetIndexType())
		prop, ok := logsIndex.GetMappings().GetProperty("service.name")
		assert.True(t, ok)
		assert.Equal(t, "keyword", prop.Type)

		products, _ := core.ZED_INDEX_LIST.Get(prefix + "products")
		prop, _ = products.GetMappings().GetProperty("embedding")
		assert.Equal(t, "vector", prop.Type)
		assert.Equal(t, 4, prop.Dims)

		res := search(t, prefix+"stores", `{"query": {"geo_distance": {"distance": "500km", "location": {"lat": 37.7749, "lon": -122.4194}}}}`)
		assert.Equal(t, counts["stores"], res.Hits.Total.Value)

		res = search(t, prefix+"forum", `{"query": {"nested": {"path": "comments", "query": {"range": {"comments.stars": {"gte": 1}}}}}}`)
		assert.Equal(t, 3, res.Hits.Total.Value) // questions 1, 5, 9

		res = search(t, prefix+"forum", `{"query": {"has_parent": {"parent_type": "question", "query": {"match_all": {}}}}}`)
		assert.Equal(t, 6, res.Hits.Total.Value)
	})

	for _, d := range datasets {
		assert.NoError(t, core.DeleteIndex(prefix+d.name))
	}
}
