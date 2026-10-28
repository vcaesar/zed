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
	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/search"

	zincaggregation "github.com/vcaesar/zedsearch/pkg/bluge/aggregation"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// testSearchAggregation collects what Request registers.
type testSearchAggregation struct {
	aggs map[string]search.Aggregation
}

func newTestSearchAggregation() *testSearchAggregation {
	return &testSearchAggregation{aggs: make(map[string]search.Aggregation)}
}

func (t *testSearchAggregation) AddAggregation(name string, aggregation search.Aggregation) {
	t.aggs[name] = aggregation
}

// stubMatchReader serves hand-built doc values so Response can be exercised
// without opening an index.
type stubMatchReader struct {
	docVals map[string][][]byte
}

func (r *stubMatchReader) DocumentValueReader(_ []string) (segment.DocumentValueReader, error) {
	return r, nil
}

func (r *stubMatchReader) VisitDocumentValues(_ uint64, visitor segment.DocumentValueVisitor) error {
	for field, vals := range r.docVals {
		for _, v := range vals {
			visitor(field, v)
		}
	}
	return nil
}

func (r *stubMatchReader) VisitStoredFields(_ uint64, _ segment.StoredFieldVisitor) error {
	return nil
}

func testMappings() *meta.Mappings {
	mappings := meta.NewMappings()
	mappings.SetProperty("age", meta.NewProperty("numeric"))
	mappings.SetProperty("name", meta.NewProperty("text"))
	return mappings
}

func numTerms(vs ...float64) [][]byte {
	rv := make([][]byte, 0, len(vs))
	for _, v := range vs {
		rv = append(rv, numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(v), 0))
	}
	return rv
}

func newTestMatch(t *testing.T, fields []string, docVals map[string][][]byte) *search.DocumentMatch {
	t.Helper()
	d := &search.DocumentMatch{Number: 1, Score: 1}
	d.SetReader(&stubMatchReader{docVals: docVals})
	require.NoError(t, d.LoadDocumentValues(search.NewSearchContext(0, 0), fields))
	return d
}

// ageDocs builds one match per value on the numeric field "age".
func ageDocs(t *testing.T, values ...float64) []*search.DocumentMatch {
	t.Helper()
	docs := make([]*search.DocumentMatch, 0, len(values))
	for _, v := range values {
		docs = append(docs, newTestMatch(t, []string{"age", "name"}, map[string][][]byte{"age": numTerms(v)}))
	}
	return docs
}

// responseFor runs Request then feeds docs through a bucket and renders it.
func responseFor(t *testing.T, aggs map[string]meta.Aggregations, docs []*search.DocumentMatch) map[string]meta.AggregationResponse {
	t.Helper()
	req := newTestSearchAggregation()
	require.NoError(t, Request(req, aggs, testMappings()))

	bucket := search.NewBucket("test", req.aggs)
	for _, d := range docs {
		bucket.Consume(d)
	}
	bucket.Finish()

	resp, err := Response(bucket)
	require.NoError(t, err)
	return resp
}

func marshalAgg(t *testing.T, resp meta.AggregationResponse) string {
	t.Helper()
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	return string(b)
}

func TestRequestMetricAggregations(t *testing.T) {
	tests := []struct {
		name   string
		aggs   map[string]meta.Aggregations
		assert func(t *testing.T, agg search.Aggregation)
	}{
		{
			name: "stats",
			aggs: map[string]meta.Aggregations{"my_agg": {Stats: &meta.AggregationMetric{Field: "age"}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.StatsAggregation)
				require.True(t, ok)
				assert.Equal(t, []string{"age"}, a.Fields())
				calc := a.Calculator().(*zincaggregation.StatsCalculator)
				assert.False(t, calc.Extended())
			},
		},
		{
			name: "extended_stats defaults sigma to 2",
			aggs: map[string]meta.Aggregations{"my_agg": {ExtendedStats: &meta.AggregationExtendedStats{Field: "age"}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.StatsAggregation)
				require.True(t, ok)
				calc := a.Calculator().(*zincaggregation.StatsCalculator)
				assert.True(t, calc.Extended())
				assert.Equal(t, float64(2), calc.Sigma())
			},
		},
		{
			name: "extended_stats keeps explicit sigma",
			aggs: map[string]meta.Aggregations{"my_agg": {ExtendedStats: &meta.AggregationExtendedStats{Field: "age", Sigma: 3}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				calc := agg.(*zincaggregation.StatsAggregation).Calculator().(*zincaggregation.StatsCalculator)
				assert.Equal(t, float64(3), calc.Sigma())
			},
		},
		{
			name: "percentiles defaults percents",
			aggs: map[string]meta.Aggregations{"my_agg": {Percentiles: &meta.AggregationPercentiles{Field: "age"}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.PercentilesAggregation)
				require.True(t, ok)
				assert.Equal(t, []string{"age"}, a.Fields())
				calc := a.Calculator().(*zincaggregation.PercentilesCalculator)
				assert.Equal(t, zincaggregation.DefaultPercents, calc.Percents())
			},
		},
		{
			name: "percentiles keeps explicit percents",
			aggs: map[string]meta.Aggregations{"my_agg": {Percentiles: &meta.AggregationPercentiles{Field: "age", Percents: []float64{50, 90}}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				calc := agg.(*zincaggregation.PercentilesAggregation).Calculator().(*zincaggregation.PercentilesCalculator)
				assert.Equal(t, []float64{50, 90}, calc.Percents())
			},
		},
		{
			name: "value_count",
			aggs: map[string]meta.Aggregations{"my_agg": {ValueCount: &meta.AggregationMetric{Field: "age"}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.ValueCountAggregation)
				require.True(t, ok)
				assert.Equal(t, []string{"age"}, a.Fields())
			},
		},
		{
			name: "missing on a numeric field",
			aggs: map[string]meta.Aggregations{"my_agg": {Missing: &meta.AggregationMetric{Field: "age"}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				_, ok := agg.(*zincaggregation.MissingAggregation)
				require.True(t, ok)
			},
		},
		{
			name: "missing on a text field",
			aggs: map[string]meta.Aggregations{"my_agg": {Missing: &meta.AggregationMetric{Field: "name"}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				a, ok := agg.(*zincaggregation.MissingAggregation)
				require.True(t, ok)
				assert.Equal(t, []string{"name"}, a.Fields())
			},
		},
		{
			name: "missing on an unmapped field",
			aggs: map[string]meta.Aggregations{"my_agg": {Missing: &meta.AggregationMetric{Field: "nope"}}},
			assert: func(t *testing.T, agg search.Aggregation) {
				_, ok := agg.(*zincaggregation.MissingAggregation)
				require.True(t, ok)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newTestSearchAggregation()
			require.NoError(t, Request(req, tt.aggs, testMappings()))
			require.Len(t, req.aggs, 1)
			tt.assert(t, req.aggs["my_agg"])
		})
	}
}

func TestRequestMetricAggregationsError(t *testing.T) {
	tests := []struct {
		name string
		aggs map[string]meta.Aggregations
		want string
	}{
		{
			name: "stats on a text field",
			aggs: map[string]meta.Aggregations{"my_agg": {Stats: &meta.AggregationMetric{Field: "name"}}},
			want: "[stats] aggregation doesn't support values of type: [name:[text]]",
		},
		{
			name: "stats on an unmapped field",
			aggs: map[string]meta.Aggregations{"my_agg": {Stats: &meta.AggregationMetric{Field: "nope"}}},
			want: "[stats] aggregation doesn't support values of type: [nope:[]]",
		},
		{
			name: "extended_stats on a text field",
			aggs: map[string]meta.Aggregations{"my_agg": {ExtendedStats: &meta.AggregationExtendedStats{Field: "name"}}},
			want: "[extended_stats] aggregation doesn't support values of type: [name:[text]]",
		},
		{
			name: "extended_stats with a negative sigma",
			aggs: map[string]meta.Aggregations{"my_agg": {ExtendedStats: &meta.AggregationExtendedStats{Field: "age", Sigma: -1}}},
			want: "[extended_stats] aggregation sigma must not be negative",
		},
		{
			name: "percentiles on a text field",
			aggs: map[string]meta.Aggregations{"my_agg": {Percentiles: &meta.AggregationPercentiles{Field: "name"}}},
			want: "[percentiles] aggregation doesn't support values of type: [name:[text]]",
		},
		{
			name: "percentiles out of range",
			aggs: map[string]meta.Aggregations{"my_agg": {Percentiles: &meta.AggregationPercentiles{Field: "age", Percents: []float64{101}}}},
			want: "[percentiles] aggregation percents must be in [0, 100]",
		},
		{
			name: "value_count on a text field",
			aggs: map[string]meta.Aggregations{"my_agg": {ValueCount: &meta.AggregationMetric{Field: "name"}}},
			want: "[value_count] aggregation doesn't support values of type: [name:[text]]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newTestSearchAggregation()
			err := Request(req, tt.aggs, testMappings())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Empty(t, req.aggs)
		})
	}
}

func TestResponseStats(t *testing.T) {
	resp := responseFor(t,
		map[string]meta.Aggregations{"my_agg": {Stats: &meta.AggregationMetric{Field: "age"}}},
		ageDocs(t, 1, 2, 3, 4),
	)
	assert.JSONEq(t, `{"count":4,"min":1,"max":4,"avg":2.5,"sum":10}`, marshalAgg(t, resp["my_agg"]))
}

// Elasticsearch reports null for min/max/avg of an empty stats bucket, zinc
// reports 0. count must still be present.
func TestResponseStatsEmpty(t *testing.T) {
	resp := responseFor(t,
		map[string]meta.Aggregations{"my_agg": {Stats: &meta.AggregationMetric{Field: "age"}}},
		nil,
	)
	assert.JSONEq(t, `{"count":0,"min":0,"max":0,"avg":0,"sum":0}`, marshalAgg(t, resp["my_agg"]))
}

// TestResponseEmptyMinMaxIsJSONSafe pins that an empty min / max bucket does
// not leak the +/-Inf the underlying calculators start with: json.Marshal
// rejects those and the whole search response would fail to render.
func TestResponseEmptyMinMaxIsJSONSafe(t *testing.T) {
	tests := []struct {
		name string
		aggs map[string]meta.Aggregations
	}{
		{name: "min", aggs: map[string]meta.Aggregations{"my_agg": {Min: &meta.AggregationMetric{Field: "age"}}}},
		{name: "max", aggs: map[string]meta.Aggregations{"my_agg": {Max: &meta.AggregationMetric{Field: "age"}}}},
		{name: "avg", aggs: map[string]meta.Aggregations{"my_agg": {Avg: &meta.AggregationMetric{Field: "age"}}}},
		{name: "sum", aggs: map[string]meta.Aggregations{"my_agg": {Sum: &meta.AggregationMetric{Field: "age"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := responseFor(t, tt.aggs, nil)
			assert.JSONEq(t, `{"value":0}`, marshalAgg(t, resp["my_agg"]))
		})
	}
}

func TestResponseStatsMergedBuckets(t *testing.T) {
	req := newTestSearchAggregation()
	aggs := map[string]meta.Aggregations{"my_agg": {Stats: &meta.AggregationMetric{Field: "age"}}}
	require.NoError(t, Request(req, aggs, testMappings()))

	shard1 := search.NewBucket("shard1", req.aggs)
	for _, d := range ageDocs(t, 1, 2) {
		shard1.Consume(d)
	}
	shard1.Finish()

	shard2 := search.NewBucket("shard2", req.aggs)
	for _, d := range ageDocs(t, 3, 4) {
		shard2.Consume(d)
	}
	shard2.Finish()

	shard1.Merge(shard2)

	resp, err := Response(shard1)
	require.NoError(t, err)
	assert.JSONEq(t, `{"count":4,"min":1,"max":4,"avg":2.5,"sum":10}`, marshalAgg(t, resp["my_agg"]))
}

func TestResponseExtendedStats(t *testing.T) {
	resp := responseFor(t,
		map[string]meta.Aggregations{"my_agg": {ExtendedStats: &meta.AggregationExtendedStats{Field: "age"}}},
		ageDocs(t, 1, 2, 3, 4),
	)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(marshalAgg(t, resp["my_agg"])), &got))

	assert.Equal(t, float64(4), got["count"])
	assert.Equal(t, float64(1), got["min"])
	assert.Equal(t, float64(4), got["max"])
	assert.Equal(t, 2.5, got["avg"])
	assert.Equal(t, float64(10), got["sum"])
	assert.Equal(t, float64(30), got["sum_of_squares"])
	assert.InDelta(t, 1.25, got["variance"], 1e-9)
	assert.InDelta(t, 1.1180339887498949, got["std_deviation"], 1e-9)

	bounds, ok := got["std_deviation_bounds"].(map[string]interface{})
	require.True(t, ok)
	assert.InDelta(t, 2.5+2*1.1180339887498949, bounds["upper"], 1e-9)
	assert.InDelta(t, 2.5-2*1.1180339887498949, bounds["lower"], 1e-9)
}

func TestResponseExtendedStatsSigma(t *testing.T) {
	resp := responseFor(t,
		map[string]meta.Aggregations{"my_agg": {ExtendedStats: &meta.AggregationExtendedStats{Field: "age", Sigma: 3}}},
		ageDocs(t, 1, 2, 3, 4),
	)
	bounds := resp["my_agg"].StdDeviationBounds
	require.NotNil(t, bounds)
	assert.InDelta(t, 2.5+3*1.1180339887498949, bounds.Upper, 1e-9)
	assert.InDelta(t, 2.5-3*1.1180339887498949, bounds.Lower, 1e-9)
}

func TestResponsePercentiles(t *testing.T) {
	resp := responseFor(t,
		map[string]meta.Aggregations{"my_agg": {Percentiles: &meta.AggregationPercentiles{Field: "age", Percents: []float64{50, 99}}}},
		ageDocs(t, 10, 20, 30),
	)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(marshalAgg(t, resp["my_agg"])), &got))

	values, ok := got["values"].(map[string]interface{})
	require.True(t, ok)
	require.Len(t, values, 2)
	// t-digest interpolates, the exact value is approximate by design
	assert.InDelta(t, 20, values["50.0"], 1e-9)
	assert.InDelta(t, 30, values["99.0"], 0.5)
	// percentiles renders only "values", never the generic "value"
	assert.NotContains(t, got, "value")
}

func TestResponsePercentilesDefaults(t *testing.T) {
	resp := responseFor(t,
		map[string]meta.Aggregations{"my_agg": {Percentiles: &meta.AggregationPercentiles{Field: "age"}}},
		ageDocs(t, 1, 2, 3, 4, 5),
	)
	values := resp["my_agg"].Values
	require.Len(t, values, len(zincaggregation.DefaultPercents))
	for _, key := range []string{"1.0", "5.0", "25.0", "50.0", "75.0", "95.0", "99.0"} {
		assert.Contains(t, values, key)
	}
}

func TestResponseValueCount(t *testing.T) {
	docs := ageDocs(t, 1, 2, 3)
	// a document with several values for the field counts once per value
	docs = append(docs, newTestMatch(t, []string{"age", "name"}, map[string][][]byte{"age": numTerms(4, 5)}))
	// a document without the field is not counted
	docs = append(docs, newTestMatch(t, []string{"age", "name"}, map[string][][]byte{"name": {[]byte("bob")}}))

	resp := responseFor(t,
		map[string]meta.Aggregations{"my_agg": {ValueCount: &meta.AggregationMetric{Field: "age"}}},
		docs,
	)
	assert.JSONEq(t, `{"value":5}`, marshalAgg(t, resp["my_agg"]))
}

func TestResponseMissing(t *testing.T) {
	docs := ageDocs(t, 1, 2)
	docs = append(docs,
		newTestMatch(t, []string{"age", "name"}, map[string][][]byte{"name": {[]byte("bob")}}),
		newTestMatch(t, []string{"age", "name"}, map[string][][]byte{"name": {[]byte("alice")}}),
	)

	resp := responseFor(t,
		map[string]meta.Aggregations{"my_agg": {Missing: &meta.AggregationMetric{Field: "age"}}},
		docs,
	)
	assert.JSONEq(t, `{"doc_count":2}`, marshalAgg(t, resp["my_agg"]))
}

func TestResponseMissingZero(t *testing.T) {
	resp := responseFor(t,
		map[string]meta.Aggregations{"my_agg": {Missing: &meta.AggregationMetric{Field: "age"}}},
		ageDocs(t, 1, 2),
	)
	assert.JSONEq(t, `{"doc_count":0}`, marshalAgg(t, resp["my_agg"]))
}

// the new response fields must not leak into the existing metric responses
func TestResponseMetricUnchanged(t *testing.T) {
	resp := responseFor(t,
		map[string]meta.Aggregations{"my_agg": {Avg: &meta.AggregationMetric{Field: "age"}}},
		ageDocs(t, 1, 2, 3, 4),
	)
	assert.JSONEq(t, `{"value":2.5}`, marshalAgg(t, resp["my_agg"]))
}

// Values near MaxFloat64 overflow sum_of_squares to +Inf and the variance to
// NaN; the response must still marshal.
func TestResponseExtendedStatsOverflowIsJSONSafe(t *testing.T) {
	resp := responseFor(t,
		map[string]meta.Aggregations{"my_agg": {ExtendedStats: &meta.AggregationExtendedStats{Field: "age"}}},
		ageDocs(t, 1e300, 1e300),
	)
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(marshalAgg(t, resp["my_agg"])), &got))
	assert.Equal(t, float64(0), got["sum_of_squares"])
	assert.Equal(t, float64(0), got["variance"])
}
