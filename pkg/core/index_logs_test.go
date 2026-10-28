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

package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

func TestLogsIndex(t *testing.T) {
	name := "TestLogsIndex.index_1"
	index, err := NewIndex(name, "disk", 1)
	require.NoError(t, err)
	require.NoError(t, index.SetSettings(&meta.IndexSettings{IndexType: meta.IndexTypeLogs}))
	require.NoError(t, StoreIndex(index))
	t.Cleanup(func() { assert.NoError(t, DeleteIndex(name)) })

	instant := time.Date(2022, 1, 2, 3, 4, 5, 123456789, time.UTC)
	doc := map[string]interface{}{
		"@timestamp": instant.Format(time.RFC3339Nano),
		"message":    "request failed",
		"level":      "error",
		"service":    map[string]interface{}{"name": "api"},
	}
	require.NoError(t, index.CreateDocument("logstash", doc, false))
	assert.Equal(t, instant.Format(time.RFC3339Nano), doc[meta.TimeFieldName])
	require.NoError(t, index.CreateDocument("openobserve", map[string]interface{}{
		"_timestamp": float64(instant.Add(time.Hour).UnixMicro()),
		"log":        "request succeeded",
		"level":      "info",
	}, false))
	waitForDocs(t, index, 2)

	query := new(meta.ZincQuery)
	require.NoError(t, json.Unmarshal([]byte(`{
		"query":{"range":{"@timestamp":{"gte":"2022-01-02T03:00:00Z","lt":"2022-01-02T05:00:00Z"}}},
		"sort":["-@timestamp"],"size":10,
		"aggs":{"levels":{"terms":{"field":"level"}},"hours":{"date_histogram":{"field":"@timestamp","fixed_interval":"1h"}}}
	}`), query))
	res, err := index.Search(query)
	require.NoError(t, err)
	require.Len(t, res.Hits.Hits, 2)
	assert.Equal(t, "openobserve", res.Hits.Hits[0].ID)
	assert.Equal(t, instant, res.Hits.Hits[1].Timestamp, "WAL must preserve nanoseconds")
	assert.Equal(t, instant.Add(time.Hour).Truncate(time.Microsecond), res.Hits.Hits[0].Timestamp)
	aggs, err := json.Marshal(res.Aggregations)
	require.NoError(t, err)
	assert.Contains(t, string(aggs), `"key":"error"`)
	assert.Contains(t, string(aggs), `"key":"info"`)
	assert.Contains(t, string(aggs), `"hours"`)

	hit, err := index.GetDocument("logstash")
	require.NoError(t, err)
	assert.Equal(t, instant.Format(time.RFC3339Nano), hit.Source.(map[string]interface{})[meta.TimeFieldName])

	// Loading persisted metadata retains both the profile and its mappings.
	require.NoError(t, index.Close())
	require.NoError(t, LoadZincIndexesFromMetadata(meta.Version))
	index, ok := GetIndex(name)
	require.True(t, ok)
	assert.Equal(t, meta.IndexTypeLogs, index.GetIndexType())
	prop, _ := index.GetMappings().GetProperty("level")
	assert.Equal(t, "keyword", prop.Type)
	hit, err = index.GetDocument("logstash")
	require.NoError(t, err)
	assert.Equal(t, instant.Format(time.RFC3339Nano), hit.Source.(map[string]interface{})[meta.TimeFieldName])

	require.NoError(t, index.UpdateDocument("logstash", map[string]interface{}{
		"_timestamp": instant.Add(2 * time.Hour).Format(time.RFC3339Nano),
		"message":    "updated event",
	}, false))
	require.Eventually(t, func() bool {
		res, err := index.Search(&meta.ZincQuery{Size: 10, Query: &meta.Query{Match: map[string]*meta.MatchQuery{"message": {Query: "updated"}}}})
		return err == nil && len(res.Hits.Hits) == 1 && res.Hits.Hits[0].Timestamp.Equal(instant.Add(2*time.Hour))
	}, 10*time.Second, 20*time.Millisecond)
}

func TestLogsIndexValidation(t *testing.T) {
	index, err := NewIndex("TestLogsIndexValidation.index_1", "disk", 1)
	require.NoError(t, err)
	assert.Equal(t, meta.IndexTypeStandard, index.GetIndexType())
	assert.ErrorContains(t, index.SetSettings(&meta.IndexSettings{IndexType: "invalid"}), "index_type")
	require.NoError(t, index.SetSettings(&meta.IndexSettings{IndexType: meta.IndexTypeLogs}))
	require.NoError(t, StoreIndex(index))
	t.Cleanup(func() { assert.NoError(t, DeleteIndex(index.GetName())) })

	mappings := meta.NewMappings()
	mappings.SetProperty(meta.TimeFieldName, meta.NewProperty("text"))
	assert.ErrorContains(t, index.SetMappings(mappings), "type [date]")
	prop, _ := index.GetMappings().GetProperty(meta.TimeFieldName)
	assert.Equal(t, "date", prop.Type)

	shard := index.GetShardByDocID("bad")
	_, err = shard.CheckDocument("bad", map[string]interface{}{"@timestamp": "bad", "new_field": true}, false, ShardIDNeedLatest)
	assert.ErrorContains(t, err, "@timestamp")
	_, exists := index.GetMappings().GetProperty("new_field")
	assert.False(t, exists, "invalid timestamps must fail before updating mappings")
	_, err = shard.CheckDocument("null", nil, false, ShardIDNeedLatest)
	assert.ErrorContains(t, err, "object")

	// An explicit _timestamp date mapping's format/time_zone applies to the alias.
	alias := meta.NewProperty("date")
	alias.Format, alias.TimeZone = "2006-01-02 15:04:05", "America/Los_Angeles"
	aliasMappings := meta.NewMappings()
	aliasMappings.SetProperty("_timestamp", alias)
	require.NoError(t, index.SetMappings(aliasMappings))
	data, err := shard.CheckDocument("alias", map[string]interface{}{"_timestamp": "2022-01-01 19:04:05"}, false, ShardIDNeedLatest)
	require.NoError(t, err)
	wal := map[string]interface{}{}
	require.NoError(t, json.Unmarshal(data, &wal))
	assert.Equal(t, "2022-01-02T03:04:05Z", wal[meta.TimeFieldName])

	// Standard indexes keep their old behavior and don't acquire log mappings.
	standard, err := NewIndex("TestLogsIndexValidation.standard", "disk", 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(standard))
	t.Cleanup(func() { assert.NoError(t, DeleteIndex(standard.GetName())) })
	_, exists = standard.GetMappings().GetProperty("message")
	assert.False(t, exists)
	_, err = standard.GetShardByDocID("null").CheckDocument("null", map[string]interface{}{"@timestamp": nil}, false, ShardIDNeedLatest)
	assert.Error(t, err)
}

func TestLogsIndexTemplate(t *testing.T) {
	name := "TestLogsIndexTemplate"
	require.NoError(t, NewTemplate(name, &meta.IndexTemplate{
		IndexPatterns: []string{name + ".*"},
		Template: meta.TemplateTemplate{
			Settings: &meta.IndexSettings{IndexType: meta.IndexTypeLogs},
		},
	}))
	t.Cleanup(func() { assert.NoError(t, DeleteTemplate(name)) })
	index, exists, err := GetOrCreateIndex(name+".index_1", "disk", 1)
	require.NoError(t, err)
	assert.False(t, exists)
	t.Cleanup(func() { assert.NoError(t, DeleteIndex(index.GetName())) })
	assert.Equal(t, meta.IndexTypeLogs, index.GetIndexType())
	prop, exists := index.GetMappings().GetProperty("level")
	assert.True(t, exists)
	assert.Equal(t, "keyword", prop.Type)

	bad := meta.NewMappings()
	bad.SetProperty(meta.TimeFieldName, meta.NewProperty("text"))
	require.NoError(t, NewTemplate(name, &meta.IndexTemplate{
		IndexPatterns: []string{name + ".*"},
		Template:      meta.TemplateTemplate{Settings: &meta.IndexSettings{IndexType: meta.IndexTypeLogs}, Mappings: bad},
	}))
	_, err = NewIndex(name+".invalid", "disk", 1)
	assert.ErrorContains(t, err, "type [date]")
}
