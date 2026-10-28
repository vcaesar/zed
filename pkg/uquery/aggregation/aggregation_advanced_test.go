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

package aggregation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot/search"

	zincaggregation "github.com/vcaesar/zedsearch/pkg/bluge/aggregation"
	"github.com/vcaesar/zedsearch/pkg/meta"
)

// storedStubReader adds stored fields to the doc value stub, so [top_hits]
// can be rendered without opening an index.
type storedStubReader struct {
	docVals map[string][][]byte
	stored  map[string]string
}

func (r *storedStubReader) DocumentValueReader(_ []string) (segment.DocumentValueReader, error) {
	return r, nil
}

func (r *storedStubReader) VisitDocumentValues(_ uint64, visitor segment.DocumentValueVisitor) error {
	for field, vals := range r.docVals {
		for _, v := range vals {
			visitor(field, v)
		}
	}
	return nil
}

func (r *storedStubReader) VisitStoredFields(_ uint64, visitor segment.StoredFieldVisitor) error {
	for field, v := range r.stored {
		visitor(field, []byte(v))
	}
	return nil
}

func advancedMappings() *meta.Mappings {
	mappings := meta.NewMappings()
	mappings.SetProperty("age", meta.NewProperty("numeric"))
	mappings.SetProperty("name", meta.NewProperty("text"))
	mappings.SetProperty("city", meta.NewProperty("keyword"))
	mappings.SetProperty(meta.NestedPathField, meta.NewProperty("keyword"))
	return mappings
}

// cityDocs builds one match per (id, city, age) triple, with stored fields.
func cityDocs(t *testing.T, ids, cities []string, ages []float64) []*search.DocumentMatch {
	t.Helper()
	docs := make([]*search.DocumentMatch, 0, len(ids))
	for i, id := range ids {
		d := &search.DocumentMatch{Number: uint64(i + 1), Score: float64(len(ids) - i)}
		d.SetReader(&storedStubReader{
			docVals: map[string][][]byte{"city": {[]byte(cities[i])}, "age": numTerms(ages[i])},
			stored:  map[string]string{"_id": id, "_index": "idx", "_source": `{"city":"` + cities[i] + `"}`},
		})
		require.NoError(t, d.LoadDocumentValues(search.NewSearchContext(0, 0), []string{"city", "age"}))
		docs = append(docs, d)
	}
	return docs
}

// advancedResponse runs Request then feeds docs through a bucket and renders it.
func advancedResponse(t *testing.T, aggs map[string]meta.Aggregations, docs []*search.DocumentMatch) map[string]meta.AggregationResponse {
	t.Helper()
	req := newTestSearchAggregation()
	require.NoError(t, Request(req, aggs, advancedMappings()))

	bucket := search.NewBucket("test", req.aggs)
	for _, d := range docs {
		bucket.Consume(d)
	}
	bucket.Finish()

	resp, err := Response(bucket)
	require.NoError(t, err)
	return resp
}

func TestRequestBucketAggregations(t *testing.T) {
	tests := []struct {
		name   string
		aggs   map[string]meta.Aggregations
		assert func(t *testing.T, agg search.Aggregation)
	}{
		{
			name: "filter",
			aggs: map[string]meta.Aggregations{"my_agg": {
				Filter: map[string]interface{}{"term": map[string]interface{}{"city": map[string]interface{}{"value": "sf"}}},
			}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.FilterAggregation)
				require.True(t, ok)
				assert.Contains(t, a.Fields(), "city")
			},
		},
		{
			name: "filters with other bucket",
			aggs: map[string]meta.Aggregations{"my_agg": {Filters: &meta.AggregationFilters{
				Filters: map[string]interface{}{
					"bay":   map[string]interface{}{"term": map[string]interface{}{"city": map[string]interface{}{"value": "sf"}}},
					"socal": map[string]interface{}{"term": map[string]interface{}{"city": map[string]interface{}{"value": "la"}}},
				},
				OtherBucketKey: "other",
			}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.FiltersAggregation)
				require.True(t, ok)
				calc := a.Calculator().(*zincaggregation.FiltersCalculator)
				names := []string{}
				for _, b := range calc.NamedBuckets() {
					names = append(names, b.Name())
				}
				assert.Equal(t, []string{"bay", "socal", "other"}, names)
			},
		},
		{
			// other_bucket alone asks for the other bucket under the default
			// key, elasticsearch only needs other_bucket_key to rename it.
			name: "filters with other bucket and no key",
			aggs: map[string]meta.Aggregations{"my_agg": {Filters: &meta.AggregationFilters{
				Filters: map[string]interface{}{
					"bay": map[string]interface{}{"term": map[string]interface{}{"city": map[string]interface{}{"value": "sf"}}},
				},
				OtherBucket: true,
			}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.FiltersAggregation)
				require.True(t, ok)
				calc := a.Calculator().(*zincaggregation.FiltersCalculator)
				names := []string{}
				for _, b := range calc.NamedBuckets() {
					names = append(names, b.Name())
				}
				assert.Equal(t, []string{"bay", "_other_"}, names)
			},
		},
		{
			name: "filters without other bucket",
			aggs: map[string]meta.Aggregations{"my_agg": {Filters: &meta.AggregationFilters{
				Filters: map[string]interface{}{
					"bay": map[string]interface{}{"term": map[string]interface{}{"city": map[string]interface{}{"value": "sf"}}},
				},
			}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.FiltersAggregation)
				require.True(t, ok)
				calc := a.Calculator().(*zincaggregation.FiltersCalculator)
				require.Len(t, calc.NamedBuckets(), 1)
				assert.Equal(t, "bay", calc.NamedBuckets()[0].Name())
			},
		},
		{
			name: "multi_terms",
			aggs: map[string]meta.Aggregations{"my_agg": {MultiTerms: &meta.AggregationMultiTerms{
				Terms: []meta.AggregationMetric{{Field: "city"}, {Field: "age"}},
				Size:  5,
			}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.MultiTermsAggregation)
				require.True(t, ok)
				assert.Contains(t, a.Fields(), "city")
				assert.Contains(t, a.Fields(), "age")
			},
		},
		{
			name: "top_hits defaults size to 3",
			aggs: map[string]meta.Aggregations{"my_agg": {TopHits: &meta.AggregationTopHits{}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.TopHitsAggregation)
				require.True(t, ok)
				assert.Nil(t, a.Fields()) // sorting by _score needs no doc value
			},
		},
		{
			name: "top_hits with a sort field",
			aggs: map[string]meta.Aggregations{"my_agg": {TopHits: &meta.AggregationTopHits{
				Size: 1,
				Sort: []interface{}{map[string]interface{}{"age": "desc"}},
			}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				assert.Equal(t, []string{"age"}, agg.(*zincaggregation.TopHitsAggregation).Fields())
			},
		},
		{
			name: "percentile_ranks",
			aggs: map[string]meta.Aggregations{"my_agg": {PercentileRanks: &meta.AggregationPercentileRanks{
				Field:  "age",
				Values: []float64{20, 40},
			}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.PercentileRanksAggregation)
				require.True(t, ok)
				assert.Equal(t, []string{"age"}, a.Fields())
			},
		},
		{
			name: "nested",
			aggs: map[string]meta.Aggregations{"my_agg": {Nested: &meta.AggregationNested{Path: "comments"}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.FilterAggregation)
				require.True(t, ok)
				assert.Contains(t, a.Fields(), meta.NestedPathField)
			},
		},
		{
			name: "reverse_nested",
			aggs: map[string]meta.Aggregations{"my_agg": {ReverseNested: &meta.AggregationNested{}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				_, ok := agg.(*zincaggregation.FilterAggregation)
				require.True(t, ok)
			},
		},
		{
			name: "terms with an order",
			aggs: map[string]meta.Aggregations{"my_agg": {Terms: &meta.AggregationsTerms{
				Field: "city",
				Size:  3,
				Order: map[string]string{"_key": "asc"},
			}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				_, ok := agg.(*zincaggregation.TermsAggregation)
				require.True(t, ok)
			},
		},
		{
			name: "pipeline aggregations register nothing",
			aggs: map[string]meta.Aggregations{"my_agg": {
				AvgBucket: &meta.AggregationPipeline{BucketsPath: "hist>sales"},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newTestSearchAggregation()
			require.NoError(t, Request(req, tt.aggs, advancedMappings()))
			if tt.assert == nil {
				assert.Empty(t, req.aggs)
				return
			}
			agg, ok := req.aggs["my_agg"]
			require.True(t, ok)
			tt.assert(t, agg)
		})
	}
}

func TestRequestBucketAggregationErrors(t *testing.T) {
	tests := []struct {
		name string
		aggs map[string]meta.Aggregations
		want string
	}{
		{
			name: "global is not implemented",
			aggs: map[string]meta.Aggregations{"my_agg": {Global: &struct{}{}}},
			want: "requires a second match_all search",
		},
		{
			name: "filter needs a query",
			aggs: map[string]meta.Aggregations{"my_agg": {Filter: map[string]interface{}{}}},
			want: "query is empty",
		},
		{
			name: "filter rejects other query types",
			aggs: map[string]meta.Aggregations{"my_agg": {
				Filter: map[string]interface{}{"prefix": map[string]interface{}{"city": map[string]interface{}{"value": "s"}}},
			}},
			want: "[prefix] query",
		},
		{
			name: "filters needs filters",
			aggs: map[string]meta.Aggregations{"my_agg": {Filters: &meta.AggregationFilters{}}},
			want: "[filters] aggregation needs filters",
		},
		{
			name: "multi_terms needs two fields",
			aggs: map[string]meta.Aggregations{"my_agg": {MultiTerms: &meta.AggregationMultiTerms{
				Terms: []meta.AggregationMetric{{Field: "city"}},
			}}},
			want: "at least two fields",
		},
		{
			name: "multi_terms rejects unmapped fields",
			aggs: map[string]meta.Aggregations{"my_agg": {MultiTerms: &meta.AggregationMultiTerms{
				Terms: []meta.AggregationMetric{{Field: "city"}, {Field: "nope"}},
			}}},
			want: "doesn't support values of type",
		},
		{
			name: "top_hits rejects several sort keys",
			aggs: map[string]meta.Aggregations{"my_agg": {TopHits: &meta.AggregationTopHits{
				Sort: []interface{}{"age", "city"},
			}}},
			want: "single sort key",
		},
		{
			name: "percentile_ranks needs a numeric field",
			aggs: map[string]meta.Aggregations{"my_agg": {PercentileRanks: &meta.AggregationPercentileRanks{
				Field:  "city",
				Values: []float64{1},
			}}},
			want: "doesn't support values of type",
		},
		{
			name: "percentile_ranks needs values",
			aggs: map[string]meta.Aggregations{"my_agg": {PercentileRanks: &meta.AggregationPercentileRanks{Field: "age"}}},
			want: "[percentile_ranks] aggregation needs values",
		},
		{
			name: "nested needs a path",
			aggs: map[string]meta.Aggregations{"my_agg": {Nested: &meta.AggregationNested{}}},
			want: "[nested] aggregation needs path",
		},
		{
			name: "terms order must be asc or desc",
			aggs: map[string]meta.Aggregations{"my_agg": {Terms: &meta.AggregationsTerms{
				Field: "city",
				Order: map[string]string{"_key": "up"},
			}}},
			want: "must be asc or desc",
		},
		{
			name: "range filter needs a numeric field",
			aggs: map[string]meta.Aggregations{"my_agg": {
				Filter: map[string]interface{}{"range": map[string]interface{}{"city": map[string]interface{}{"gte": 1}}},
			}},
			want: "range query doesn't support values of type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Request(newTestSearchAggregation(), tt.aggs, advancedMappings())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestResponseBucketAggregations(t *testing.T) {
	docs := cityDocs(t,
		[]string{"1", "2", "3", "4"},
		[]string{"sf", "la", "sf", "nyc"},
		[]float64{10, 20, 30, 40},
	)

	t.Run("filter", func(t *testing.T) {
		resp := advancedResponse(t, map[string]meta.Aggregations{"bay": {
			Filter:       map[string]interface{}{"term": map[string]interface{}{"city": map[string]interface{}{"value": "sf"}}},
			Aggregations: map[string]meta.Aggregations{"avg_age": {Avg: &meta.AggregationMetric{Field: "age"}}},
		}}, docs)
		assert.JSONEq(t, `{"doc_count":2,"avg_age":{"value":20}}`, marshalAgg(t, resp["bay"]))
	})

	t.Run("filters", func(t *testing.T) {
		resp := advancedResponse(t, map[string]meta.Aggregations{"by_city": {Filters: &meta.AggregationFilters{
			Filters: map[string]interface{}{
				"bay":   map[string]interface{}{"term": map[string]interface{}{"city": map[string]interface{}{"value": "sf"}}},
				"socal": map[string]interface{}{"terms": map[string]interface{}{"city": []interface{}{"la"}}},
			},
			OtherBucketKey: "other",
		}}}, docs)
		assert.JSONEq(t,
			`{"buckets":{"bay":{"doc_count":2},"socal":{"doc_count":1},"other":{"doc_count":1}}}`,
			marshalAgg(t, resp["by_city"]),
		)
	})

	t.Run("multi_terms", func(t *testing.T) {
		resp := advancedResponse(t, map[string]meta.Aggregations{"combo": {MultiTerms: &meta.AggregationMultiTerms{
			Terms: []meta.AggregationMetric{{Field: "city"}, {Field: "age"}},
			Size:  2,
			Order: map[string]string{"_key": "asc"},
		}}}, docs)
		assert.JSONEq(t, `{"buckets":[
			{"key":["la","20"],"key_as_string":"la|20","doc_count":1},
			{"key":["nyc","40"],"key_as_string":"nyc|40","doc_count":1}
		]}`, marshalAgg(t, resp["combo"]))
	})

	t.Run("percentile_ranks", func(t *testing.T) {
		resp := advancedResponse(t, map[string]meta.Aggregations{"ranks": {
			PercentileRanks: &meta.AggregationPercentileRanks{Field: "age", Values: []float64{25}},
		}}, docs)
		require.NotNil(t, resp["ranks"].Values)
		assert.Contains(t, resp["ranks"].Values, "25.0")
	})

	t.Run("top_hits", func(t *testing.T) {
		resp := advancedResponse(t, map[string]meta.Aggregations{"best": {
			TopHits: &meta.AggregationTopHits{Size: 2},
		}}, docs)
		hits := resp["best"].Hits
		require.NotNil(t, hits)
		assert.Equal(t, 4, hits.Total.Value)
		require.Len(t, hits.Hits, 2)
		assert.Equal(t, "1", hits.Hits[0].ID) // best score first
		assert.Equal(t, map[string]interface{}{"city": "sf"}, hits.Hits[0].Source)
	})

	t.Run("top_hits with _source filtering", func(t *testing.T) {
		resp := advancedResponse(t, map[string]meta.Aggregations{"best": {
			TopHits: &meta.AggregationTopHits{Size: 1, Source: false},
		}}, docs)
		require.Len(t, resp["best"].Hits.Hits, 1)
		assert.Empty(t, resp["best"].Hits.Hits[0].Source)
	})

	t.Run("terms order by key", func(t *testing.T) {
		resp := advancedResponse(t, map[string]meta.Aggregations{"cities": {Terms: &meta.AggregationsTerms{
			Field: "city",
			Size:  3,
			Order: map[string]string{"_key": "asc"},
		}}}, docs)
		assert.JSONEq(t, `{"buckets":[
			{"key":"la","doc_count":1},
			{"key":"nyc","doc_count":1},
			{"key":"sf","doc_count":2}
		]}`, marshalAgg(t, resp["cities"]))
	})
}

func TestFilterPredicateQueries(t *testing.T) {
	docs := cityDocs(t,
		[]string{"1", "2", "3"},
		[]string{"sf", "la", "nyc"},
		[]float64{10, 20, 30},
	)

	tests := []struct {
		name  string
		query map[string]interface{}
		want  uint64
	}{
		{name: "match_all", query: map[string]interface{}{"match_all": map[string]interface{}{}}, want: 3},
		{name: "match_none", query: map[string]interface{}{"match_none": map[string]interface{}{}}, want: 0},
		{name: "exists", query: map[string]interface{}{"exists": map[string]interface{}{"field": "city"}}, want: 3},
		{
			name:  "term",
			query: map[string]interface{}{"term": map[string]interface{}{"city": map[string]interface{}{"value": "sf"}}},
			want:  1,
		},
		{
			name:  "match",
			query: map[string]interface{}{"match": map[string]interface{}{"city": map[string]interface{}{"query": "la"}}},
			want:  1,
		},
		{
			name:  "terms",
			query: map[string]interface{}{"terms": map[string]interface{}{"city": []interface{}{"sf", "la"}}},
			want:  2,
		},
		{
			name:  "range",
			query: map[string]interface{}{"range": map[string]interface{}{"age": map[string]interface{}{"gte": 20}}},
			want:  2,
		},
		{
			name: "bool must and must_not",
			query: map[string]interface{}{"bool": map[string]interface{}{
				"must":     map[string]interface{}{"range": map[string]interface{}{"age": map[string]interface{}{"gte": 10}}},
				"must_not": map[string]interface{}{"term": map[string]interface{}{"city": map[string]interface{}{"value": "nyc"}}},
			}},
			want: 2,
		},
		{
			name: "bool should",
			query: map[string]interface{}{"bool": map[string]interface{}{
				"should": []interface{}{
					map[string]interface{}{"term": map[string]interface{}{"city": map[string]interface{}{"value": "sf"}}},
					map[string]interface{}{"term": map[string]interface{}{"city": map[string]interface{}{"value": "nyc"}}},
				},
			}},
			want: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := advancedResponse(t, map[string]meta.Aggregations{"my_agg": {Filter: tt.query}}, docs)
			require.NotNil(t, resp["my_agg"].DocCount)
			assert.Equal(t, tt.want, *resp["my_agg"].DocCount)
		})
	}
}
