/* Copyright 2022 Zinc Labs Inc. and Contributors
* Copyright 2026 AtomAI
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
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vcaesar/riot"

	"github.com/vcaesar/zedsearch/pkg/meta"
)

func TestIndex_Search(t *testing.T) {
	type args struct {
		iQuery *meta.ZedQuery
	}
	tests := []struct {
		name    string
		args    args
		want    *meta.SearchResponse
		wantNum int
		wantErr bool
	}{
		{
			name: "Search Query - Match",
			args: args{
				iQuery: &meta.ZedQuery{
					Query: &meta.Query{
						Match: map[string]*meta.MatchQuery{
							"_all": {
								Query: "Prabhat",
							},
						},
					},
					Size: 10,
				},
			},
		},
		{
			name: "Search Query - Term",
			args: args{
				iQuery: &meta.ZedQuery{
					Query: &meta.Query{
						Term: map[string]*meta.TermQuery{
							"_all": {
								Value: "angeles",
							},
						},
					},
					Size: 10,
				},
			},
		},
		{
			name: "Search Query - MatchAll",
			args: args{
				iQuery: &meta.ZedQuery{
					Query: &meta.Query{
						MatchAll: &meta.MatchAllQuery{},
					},
					Size: 10,
				},
			},
			wantNum: 3,
		},
		{
			name: "Search Query - wildcard",
			args: args{
				iQuery: &meta.ZedQuery{
					Query: &meta.Query{
						Wildcard: map[string]*meta.WildcardQuery{
							"_all": {
								Value: "san*",
							},
						},
					},
					Size: 10,
				},
			},
		},
		{
			name: "Search Query - fuzzy",
			args: args{
				iQuery: &meta.ZedQuery{
					Query: &meta.Query{
						Fuzzy: map[string]*meta.FuzzyQuery{
							"_all": {
								Value: "fransisco", // note the wrong spelling
							},
						},
					},
					Size: 10,
				},
			},
		},
		{
			name: "Search Query - fuzzy fuzziness AUTO",
			args: args{
				iQuery: &meta.ZedQuery{
					Query: &meta.Query{
						Fuzzy: map[string]*meta.FuzzyQuery{
							"_all": {
								Value:     "fransisco", // note the wrong spelling,
								Fuzziness: "AUTO",
							},
						},
					},
					Size: 10,
				},
			},
		},
		{
			name: "Search Query - fuzzy fuzziness AUTO",
			args: args{
				iQuery: &meta.ZedQuery{
					Query: &meta.Query{
						Fuzzy: map[string]*meta.FuzzyQuery{
							"_all": {
								Value:     "fransisco", // note the wrong spelling,
								Fuzziness: "AUTO:3,6",
							},
						},
					},
					Size: 10,
				},
			},
		},
		{
			name: "Search Query - fuzzy fuzziness 2",
			args: args{
				iQuery: &meta.ZedQuery{
					Query: &meta.Query{
						Fuzzy: map[string]*meta.FuzzyQuery{
							"_all": {
								Value:     "fransisco", // note the wrong spelling,
								Fuzziness: 2,
							},
						},
					},
					Size: 10,
				},
			},
		},
		{
			name: "Search Query - querystring",
			args: args{
				iQuery: &meta.ZedQuery{
					Query: &meta.Query{
						QueryString: &meta.QueryStringQuery{
							Query: "angeles",
						},
					},
					Size: 10,
				},
			},
		},
		{
			name: "Search Query - highlight",
			args: args{
				iQuery: &meta.ZedQuery{
					Query: &meta.Query{
						QueryString: &meta.QueryStringQuery{
							Query: "angeles",
						},
					},
					Timeout: 1,
					Size:    10,
					Fields:  []interface{}{"address.city"},
					Highlight: &meta.Highlight{
						Fields: map[string]*meta.Highlight{
							"address.city": {
								PreTags:  []string{"<b>"},
								PostTags: []string{"</b>"},
							},
						},
					},
				},
			},
		},
		{
			name: "Search Query - aggs",
			args: args{
				iQuery: &meta.ZedQuery{
					Query: &meta.Query{
						MatchAll: &meta.MatchAllQuery{},
					},
					Timeout: 1,
					Size:    0,
					Aggregations: map[string]meta.Aggregations{
						"hobby": {
							Terms: &meta.AggregationsTerms{
								Field: "hobby",
							},
						},
					},
				},
			},
		},
	}

	prepareData := []map[string]interface{}{
		{
			"name": "Prabhat Sharma",
			"address": map[string]interface{}{
				"city":  "San Francisco",
				"state": "California",
			},
			"hobby": "chess",
		},
		{
			"name": "Leonardo DiCaprio",
			"address": map[string]interface{}{
				"city":  "Los angeles",
				"state": "California",
			},
			"hobby": "chess",
		},
		{
			"name": "Baris DiCaprio",
			"address": map[string]interface{}{
				"city":  "Los angeles",
				"state": "California",
			},
			"hobby": "chess",
		},
	}

	var err error
	var index *Index
	indexName := "Search.v2.index_1"
	t.Run("Prepare", func(t *testing.T) {
		index, err = NewIndex(indexName, "disk", 2)
		assert.NoError(t, err)
		assert.NotNil(t, index)
		err = StoreIndex(index)
		assert.NoError(t, err)

		index.GetMappings().SetProperty("address.city", meta.Property{
			Type:          "text",
			Index:         true,
			Store:         true,
			Highlightable: true,
		})

		for i, d := range prepareData {
			err := index.CreateDocument(strconv.Itoa(i), d, false)
			assert.NoError(t, err)
		}

		waitForDocs(t, index, len(prepareData))
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := index.Search(tt.args.iQuery)
			assert.NoError(t, err)
			assert.GreaterOrEqual(t, got.Hits.Total.Value, 1)
			if tt.wantNum > 0 {
				assert.Equal(t, got.Hits.Total.Value, tt.wantNum)
				assert.Equal(t, len(got.Hits.Hits), tt.wantNum)
			}
		})
	}

	t.Run("Cleanup", func(t *testing.T) {
		err = DeleteIndex(indexName)
		assert.NoError(t, err)
	})
}

// waitForDocs waits until all shards have consumed the WAL and n documents are searchable.
func waitForDocs(t *testing.T, index *Index, n int) {
	t.Helper()
	query := &meta.ZedQuery{Query: &meta.Query{MatchAll: &meta.MatchAllQuery{}}, Size: n + 1}
	assert.Eventually(t, func() bool {
		res, err := index.Search(query)
		return err == nil && res.Hits.Total.Value == n
	}, 30*time.Second, 50*time.Millisecond, "expected %d documents to be indexed", n)
}

func TestSearchV2_ResetsFieldsPerHit(t *testing.T) {
	w, err := riot.OpenWriter(riot.InMemoryOnlyConfig())
	require.NoError(t, err)
	defer func() { assert.NoError(t, w.Close()) }()

	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	batch := riot.NewBatch()
	first := riot.NewDocument("a").
		AddField(riot.NewNumericField("n", 1).Sortable()).
		AddField(riot.NewStoredOnlyField("_index", []byte("idx"))).
		AddField(riot.NewDateTimeField("@timestamp", ts).StoreValue()).
		AddField(riot.NewStoredOnlyField("_source", []byte(`{"x":1}`)))
	second := riot.NewDocument("b").
		AddField(riot.NewNumericField("n", 2).Sortable()).
		AddField(riot.NewStoredOnlyField("_source", []byte(`{"x":2}`)))
	batch.Update(first.ID(), first)
	batch.Update(second.ID(), second)
	require.NoError(t, w.Batch(batch))

	r, err := w.Reader()
	require.NoError(t, err)
	defer func() { assert.NoError(t, r.Close()) }()
	req := riot.NewTopNSearch(10, riot.NewMatchAllQuery()).SortBy([]string{"n"}).WithStandardAggregations()
	dmi, err := r.Search(context.Background(), req)
	require.NoError(t, err)

	query := &meta.ZedQuery{Source: &meta.Source{Enable: true}}
	resp, err := searchV2(1, 1, dmi, query, meta.NewMappings())
	require.NoError(t, err)
	require.Len(t, resp.Hits.Hits, 2)

	got := resp.Hits.Hits
	assert.Equal(t, "a", got[0].ID)
	assert.Equal(t, "idx", got[0].Index)
	assert.True(t, ts.Equal(got[0].Timestamp))
	assert.Equal(t, float64(1), got[0].Source.(map[string]interface{})["x"])

	assert.Equal(t, "b", got[1].ID)
	assert.Empty(t, got[1].Index, "_index must not leak from the previous hit")
	assert.True(t, got[1].Timestamp.IsZero(), "@timestamp must not leak from the previous hit")
	assert.Equal(t, float64(2), got[1].Source.(map[string]interface{})["x"])
}
