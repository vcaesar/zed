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

package aggregation

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

// salesBucket is one bucket of the monthly date_histogram fixture. A nil sales
// value leaves the bucket without a "sales" sub-aggregation, i.e. a gap.
func salesBucket(key string, docCount uint64, sales *float64) map[string]interface{} {
	bucket := map[string]interface{}{"key": key, "doc_count": docCount}
	if sales != nil {
		bucket["sales"] = meta.AggregationResponse{Value: *sales}
	}
	return bucket
}

func f(v float64) *float64 { return &v }

// salesResponse is a monthly date_histogram with a sales sum per bucket.
func salesResponse(values ...*float64) map[string]meta.AggregationResponse {
	buckets := make([]map[string]interface{}, 0, len(values))
	months := []string{"2022-01", "2022-02", "2022-03", "2022-04"}
	for i, v := range values {
		buckets = append(buckets, salesBucket(months[i], uint64(i+1), v))
	}
	return map[string]meta.AggregationResponse{
		"sales_per_month": {Buckets: buckets},
	}
}

// histogramAggs declares the date_histogram of the fixture with sub-aggregations.
func histogramAggs(sub map[string]meta.Aggregations) map[string]meta.Aggregations {
	return map[string]meta.Aggregations{
		"sales_per_month": {
			DateHistogram: &meta.AggregationDateHistogram{Field: "date", CalendarInterval: "month"},
			Aggregations:  sub,
		},
	}
}

// bucketValues reads a parent pipeline result out of every bucket; missing
// results are reported as NaN.
func bucketValues(t *testing.T, resp map[string]meta.AggregationResponse, agg, name string) []float64 {
	t.Helper()
	buckets, ok := resp[agg].Buckets.([]map[string]interface{})
	require.True(t, ok)
	rv := make([]float64, 0, len(buckets))
	for _, bucket := range buckets {
		r, ok := bucket[name].(meta.AggregationResponse)
		if !ok || r.Value == nil {
			rv = append(rv, math.NaN())
			continue
		}
		v, err := toFloat(r.Value)
		require.NoError(t, err)
		rv = append(rv, v)
	}
	return rv
}

func bucketKeys(t *testing.T, resp map[string]meta.AggregationResponse, agg string) []string {
	t.Helper()
	buckets, ok := resp[agg].Buckets.([]map[string]interface{})
	require.True(t, ok)
	rv := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		rv = append(rv, bucket["key"].(string))
	}
	return rv
}

func TestApplyPipelinesSiblings(t *testing.T) {
	tests := []struct {
		name string
		agg  meta.Aggregations
		want meta.AggregationResponse
	}{
		{
			name: "avg_bucket",
			agg:  meta.Aggregations{AvgBucket: &meta.AggregationPipeline{BucketsPath: "sales_per_month>sales"}},
			want: meta.AggregationResponse{Value: float64(20)},
		},
		{
			name: "sum_bucket",
			agg:  meta.Aggregations{SumBucket: &meta.AggregationPipeline{BucketsPath: "sales_per_month>sales"}},
			want: meta.AggregationResponse{Value: float64(80)},
		},
		{
			name: "min_bucket",
			agg:  meta.Aggregations{MinBucket: &meta.AggregationPipeline{BucketsPath: "sales_per_month>sales"}},
			want: meta.AggregationResponse{Value: float64(10)},
		},
		{
			name: "max_bucket",
			agg:  meta.Aggregations{MaxBucket: &meta.AggregationPipeline{BucketsPath: "sales_per_month>sales"}},
			want: meta.AggregationResponse{Value: float64(30)},
		},
		{
			name: "_count path",
			agg:  meta.Aggregations{SumBucket: &meta.AggregationPipeline{BucketsPath: "sales_per_month>_count"}},
			want: meta.AggregationResponse{Value: float64(10)}, // 1+2+3+4
		},
		{
			name: "explicit .value metric",
			agg:  meta.Aggregations{MaxBucket: &meta.AggregationPipeline{BucketsPath: "sales_per_month>sales.value"}},
			want: meta.AggregationResponse{Value: float64(30)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := salesResponse(f(10), f(30), f(20), f(20))
			aggs := histogramAggs(nil)
			aggs["result"] = tt.agg

			require.NoError(t, ApplyPipelines(resp, aggs))
			assert.Equal(t, tt.want, resp["result"])
		})
	}
}

func TestApplyPipelinesStatsBucket(t *testing.T) {
	resp := salesResponse(f(10), f(30), f(20), f(20))
	aggs := histogramAggs(nil)
	aggs["result"] = meta.Aggregations{StatsBucket: &meta.AggregationPipeline{BucketsPath: "sales_per_month>sales"}}

	require.NoError(t, ApplyPipelines(resp, aggs))
	assert.JSONEq(t, `{"count":4,"min":10,"max":30,"avg":20,"sum":80}`, marshalAgg(t, resp["result"]))
}

// TestApplyPipelinesStatsBucketEmpty pins the elasticsearch semantics of a
// stats_bucket over a series without a single value: min / max / avg have no
// value there, reporting them as 0 would invent data.
func TestApplyPipelinesStatsBucketEmpty(t *testing.T) {
	resp := salesResponse(nil, nil)
	aggs := histogramAggs(nil)
	aggs["result"] = meta.Aggregations{StatsBucket: &meta.AggregationPipeline{BucketsPath: "sales_per_month>sales"}}

	require.NoError(t, ApplyPipelines(resp, aggs))
	assert.JSONEq(t, `{"count":0,"sum":0}`, marshalAgg(t, resp["result"]))

	result := resp["result"]
	assert.Nil(t, result.Min)
	assert.Nil(t, result.Max)
	assert.Nil(t, result.Avg)
}

func TestApplyPipelinesParents(t *testing.T) {
	tests := []struct {
		name string
		agg  meta.Aggregations
		want []float64
	}{
		{
			name: "cumulative_sum",
			agg:  meta.Aggregations{CumulativeSum: &meta.AggregationPipeline{BucketsPath: "sales"}},
			want: []float64{10, 40, 60, 80},
		},
		{
			name: "derivative omits the first bucket",
			agg:  meta.Aggregations{Derivative: &meta.AggregationPipeline{BucketsPath: "sales"}},
			want: []float64{math.NaN(), 20, -10, 0},
		},
		{
			name: "serial_diff lag 1",
			agg: meta.Aggregations{SerialDiff: &meta.AggregationSerialDiff{
				AggregationPipeline: meta.AggregationPipeline{BucketsPath: "sales"},
				Lag:                 1,
			}},
			want: []float64{math.NaN(), 20, -10, 0},
		},
		{
			name: "serial_diff lag 2",
			agg: meta.Aggregations{SerialDiff: &meta.AggregationSerialDiff{
				AggregationPipeline: meta.AggregationPipeline{BucketsPath: "sales"},
				Lag:                 2,
			}},
			want: []float64{math.NaN(), math.NaN(), 10, -10},
		},
		{
			name: "moving_fn unweightedAvg window 2",
			agg: meta.Aggregations{MovingFn: &meta.AggregationMovingFn{
				AggregationPipeline: meta.AggregationPipeline{BucketsPath: "sales"},
				Window:              2,
				Script:              "MovingFunctions.unweightedAvg(values)",
			}},
			want: []float64{math.NaN(), 10, 20, 25},
		},
		{
			name: "moving_fn shift includes the current bucket",
			agg: meta.Aggregations{MovingFn: &meta.AggregationMovingFn{
				AggregationPipeline: meta.AggregationPipeline{BucketsPath: "sales"},
				Window:              2,
				Shift:               1,
				Script:              "MovingFunctions.max(values)",
			}},
			want: []float64{10, 30, 30, 20},
		},
		{
			name: "moving_fn sum is zero on an empty window",
			agg: meta.Aggregations{MovingFn: &meta.AggregationMovingFn{
				AggregationPipeline: meta.AggregationPipeline{BucketsPath: "sales"},
				Window:              1,
				Script:              "MovingFunctions.sum(values)",
			}},
			want: []float64{0, 10, 30, 20},
		},
		{
			name: "moving_fn linearWeightedAvg",
			agg: meta.Aggregations{MovingFn: &meta.AggregationMovingFn{
				AggregationPipeline: meta.AggregationPipeline{BucketsPath: "sales"},
				Window:              2,
				Script:              "MovingFunctions.linearWeightedAvg(values)",
			}},
			// bucket 3 weights (10, 30) as (1, 2) -> 70/3, bucket 4 (30, 20) -> 70/3
			want: []float64{math.NaN(), 10, 70.0 / 3, 70.0 / 3},
		},
		{
			name: "moving_fn stdDev",
			agg: meta.Aggregations{MovingFn: &meta.AggregationMovingFn{
				AggregationPipeline: meta.AggregationPipeline{BucketsPath: "sales"},
				Window:              2,
				Script:              "MovingFunctions.stdDev(values, MovingFunctions.unweightedAvg(values))",
			}},
			want: []float64{math.NaN(), 0, 10, 5},
		},
		{
			name: "bucket_script",
			agg: meta.Aggregations{BucketScript: &meta.AggregationBucketScript{
				BucketsPath: map[string]string{"a": "sales", "b": "_count"},
				Script:      "params.a / params.b",
			}},
			want: []float64{10, 15, 20.0 / 3, 5},
		},
		{
			name: "bucket_script with a script object",
			agg: meta.Aggregations{BucketScript: &meta.AggregationBucketScript{
				BucketsPath: map[string]string{"a": "sales"},
				Script:      map[string]interface{}{"source": "params.a * 2"},
			}},
			want: []float64{20, 60, 40, 40},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := salesResponse(f(10), f(30), f(20), f(20))
			aggs := histogramAggs(map[string]meta.Aggregations{
				"sales":  {Sum: &meta.AggregationMetric{Field: "price"}},
				"result": tt.agg,
			})

			require.NoError(t, ApplyPipelines(resp, aggs))
			got := bucketValues(t, resp, "sales_per_month", "result")
			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				if math.IsNaN(tt.want[i]) {
					assert.True(t, math.IsNaN(got[i]), "bucket %d: want no value, got %v", i, got[i])
					continue
				}
				assert.InDelta(t, tt.want[i], got[i], 1e-9, "bucket %d", i)
			}
		})
	}
}

// TestApplyPipelinesBucketScriptNotFinite pins that a script result without a
// json representation is left out of the bucket: writing +Inf would make the
// whole response fail to marshal.
func TestApplyPipelinesBucketScriptNotFinite(t *testing.T) {
	resp := map[string]meta.AggregationResponse{
		"sales_per_month": {Buckets: []map[string]interface{}{
			{"key": "2022-01", "doc_count": uint64(1), "sales": meta.AggregationResponse{Value: float64(10)},
				"divisor": meta.AggregationResponse{Value: float64(0)}},
			{"key": "2022-02", "doc_count": uint64(1), "sales": meta.AggregationResponse{Value: float64(10)},
				"divisor": meta.AggregationResponse{Value: float64(2)}},
		}},
	}
	aggs := histogramAggs(map[string]meta.Aggregations{
		"sales":   {Sum: &meta.AggregationMetric{Field: "price"}},
		"divisor": {Sum: &meta.AggregationMetric{Field: "count"}},
		"result": {BucketScript: &meta.AggregationBucketScript{
			BucketsPath: map[string]string{"a": "sales", "b": "divisor"},
			Script:      "params.a / params.b",
		}},
	})

	require.NoError(t, ApplyPipelines(resp, aggs))

	buckets, ok := resp["sales_per_month"].Buckets.([]map[string]interface{})
	require.True(t, ok)
	assert.NotContains(t, buckets[0], "result")
	assert.Equal(t, meta.AggregationResponse{Value: float64(5)}, buckets[1]["result"])

	for _, bucket := range buckets {
		_, err := json.Marshal(bucket)
		require.NoError(t, err)
	}
}

func TestApplyPipelinesBucketSelector(t *testing.T) {
	resp := salesResponse(f(10), f(300), f(20), f(200))
	aggs := histogramAggs(map[string]meta.Aggregations{
		"sales": {Sum: &meta.AggregationMetric{Field: "price"}},
		"big": {BucketSelector: &meta.AggregationBucketScript{
			BucketsPath: map[string]string{"total": "sales"},
			Script:      "params.total > 100",
		}},
	})

	require.NoError(t, ApplyPipelines(resp, aggs))
	assert.Equal(t, []string{"2022-02", "2022-04"}, bucketKeys(t, resp, "sales_per_month"))
}

func TestApplyPipelinesBucketSort(t *testing.T) {
	tests := []struct {
		name string
		sort *meta.AggregationBucketSort
		want []string
	}{
		{
			name: "desc size 2",
			sort: &meta.AggregationBucketSort{
				Sort: []map[string]interface{}{{"sales": map[string]interface{}{"order": "desc"}}},
				Size: 2,
			},
			want: []string{"2022-02", "2022-03"},
		},
		{
			name: "asc with from",
			sort: &meta.AggregationBucketSort{
				Sort: []map[string]interface{}{{"sales": "asc"}},
				From: 1,
				Size: 2,
			},
			want: []string{"2022-04", "2022-03"},
		},
		{
			name: "by _count",
			sort: &meta.AggregationBucketSort{
				Sort: []map[string]interface{}{{"_count": "desc"}},
				Size: 1,
			},
			want: []string{"2022-04"},
		},
		{
			name: "truncate only",
			sort: &meta.AggregationBucketSort{Size: 2},
			want: []string{"2022-01", "2022-02"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := salesResponse(f(10), f(30), f(20), f(15))
			aggs := histogramAggs(map[string]meta.Aggregations{
				"sales":  {Sum: &meta.AggregationMetric{Field: "price"}},
				"sorted": {BucketSort: tt.sort},
			})

			require.NoError(t, ApplyPipelines(resp, aggs))
			assert.Equal(t, tt.want, bucketKeys(t, resp, "sales_per_month"))
		})
	}
}

func TestApplyPipelinesGapPolicy(t *testing.T) {
	t.Run("skip ignores gaps in siblings", func(t *testing.T) {
		resp := salesResponse(f(10), nil, f(20), f(30))
		aggs := histogramAggs(nil)
		aggs["result"] = meta.Aggregations{AvgBucket: &meta.AggregationPipeline{BucketsPath: "sales_per_month>sales"}}

		require.NoError(t, ApplyPipelines(resp, aggs))
		assert.Equal(t, meta.AggregationResponse{Value: float64(20)}, resp["result"])
	})

	t.Run("insert_zeros counts gaps as zero", func(t *testing.T) {
		resp := salesResponse(f(10), nil, f(20), f(30))
		aggs := histogramAggs(nil)
		aggs["result"] = meta.Aggregations{AvgBucket: &meta.AggregationPipeline{
			BucketsPath: "sales_per_month>sales",
			GapPolicy:   "insert_zeros",
		}}

		require.NoError(t, ApplyPipelines(resp, aggs))
		assert.Equal(t, meta.AggregationResponse{Value: float64(15)}, resp["result"])
	})

	t.Run("skip omits the parent result of a gap bucket", func(t *testing.T) {
		resp := salesResponse(f(10), nil, f(20), f(30))
		aggs := histogramAggs(map[string]meta.Aggregations{
			"sales":  {Sum: &meta.AggregationMetric{Field: "price"}},
			"result": {CumulativeSum: &meta.AggregationPipeline{BucketsPath: "sales"}},
		})

		require.NoError(t, ApplyPipelines(resp, aggs))
		got := bucketValues(t, resp, "sales_per_month", "result")
		assert.Equal(t, float64(10), got[0])
		assert.True(t, math.IsNaN(got[1]))
		assert.Equal(t, float64(30), got[2])
		assert.Equal(t, float64(60), got[3])
	})

	t.Run("insert_zeros fills the parent series", func(t *testing.T) {
		resp := salesResponse(f(10), nil, f(20), f(30))
		aggs := histogramAggs(map[string]meta.Aggregations{
			"sales": {Sum: &meta.AggregationMetric{Field: "price"}},
			"result": {CumulativeSum: &meta.AggregationPipeline{
				BucketsPath: "sales",
				GapPolicy:   "insert_zeros",
			}},
		})

		require.NoError(t, ApplyPipelines(resp, aggs))
		assert.Equal(t, []float64{10, 10, 30, 60}, bucketValues(t, resp, "sales_per_month", "result"))
	})

	t.Run("keep_values emits an explicit null", func(t *testing.T) {
		resp := salesResponse(f(10), nil)
		aggs := histogramAggs(map[string]meta.Aggregations{
			"sales": {Sum: &meta.AggregationMetric{Field: "price"}},
			"result": {CumulativeSum: &meta.AggregationPipeline{
				BucketsPath: "sales",
				GapPolicy:   "keep_values",
			}},
		})

		require.NoError(t, ApplyPipelines(resp, aggs))
		buckets := resp["sales_per_month"].Buckets.([]map[string]interface{})
		assert.Equal(t, meta.AggregationResponse{}, buckets[1]["result"])
	})
}

// A sibling pipeline declared inside a bucket aggregation reads the buckets of
// its own level, and the parents of the inner aggregation run first.
func TestApplyPipelinesNested(t *testing.T) {
	inner := func(values ...*float64) meta.AggregationResponse {
		return salesResponse(values...)["sales_per_month"]
	}
	resp := map[string]meta.AggregationResponse{
		"by_city": {Buckets: []map[string]interface{}{
			{"key": "sf", "doc_count": uint64(2), "sales_per_month": inner(f(10), f(30))},
			{"key": "la", "doc_count": uint64(1), "sales_per_month": inner(f(20), f(40))},
		}},
	}
	aggs := map[string]meta.Aggregations{
		"by_city": {
			Terms: &meta.AggregationsTerms{Field: "city"},
			Aggregations: map[string]meta.Aggregations{
				"sales_per_month": {
					DateHistogram: &meta.AggregationDateHistogram{Field: "date", CalendarInterval: "month"},
					Aggregations: map[string]meta.Aggregations{
						"sales":  {Sum: &meta.AggregationMetric{Field: "price"}},
						"cumsum": {CumulativeSum: &meta.AggregationPipeline{BucketsPath: "sales"}},
					},
				},
				"total_sales": {SumBucket: &meta.AggregationPipeline{BucketsPath: "sales_per_month>sales"}},
			},
		},
	}

	require.NoError(t, ApplyPipelines(resp, aggs))
	cityBuckets := resp["by_city"].Buckets.([]map[string]interface{})
	require.Len(t, cityBuckets, 2)

	wantTotals := []float64{40, 60}
	wantCumSums := [][]float64{{10, 40}, {20, 60}}
	for i := range cityBuckets {
		total, ok := cityBuckets[i]["total_sales"].(meta.AggregationResponse)
		require.True(t, ok)
		assert.Equal(t, wantTotals[i], total.Value)

		inner := bucketAggs(cityBuckets[i])["sales_per_month"]
		got := bucketValues(t, map[string]meta.AggregationResponse{"m": inner}, "m", "cumsum")
		assert.Equal(t, wantCumSums[i], got)
	}
}

func TestApplyPipelinesErrors(t *testing.T) {
	tests := []struct {
		name string
		aggs map[string]meta.Aggregations
		want string
	}{
		{
			name: "unknown buckets_path",
			aggs: map[string]meta.Aggregations{"result": {
				AvgBucket: &meta.AggregationPipeline{BucketsPath: "nope>sales"},
			}},
			want: "not found",
		},
		{
			name: "buckets_path is not a multi bucket aggregation",
			aggs: map[string]meta.Aggregations{"result": {
				AvgBucket: &meta.AggregationPipeline{BucketsPath: "total_sales>sales"},
			}},
			want: "is not a multi bucket aggregation",
		},
		{
			name: "unknown gap_policy",
			aggs: map[string]meta.Aggregations{"result": {
				AvgBucket: &meta.AggregationPipeline{BucketsPath: "sales_per_month>sales", GapPolicy: "nope"},
			}},
			want: "gap_policy must be",
		},
		{
			name: "moving_fn needs a window",
			aggs: histogramAggs(map[string]meta.Aggregations{"result": {MovingFn: &meta.AggregationMovingFn{
				AggregationPipeline: meta.AggregationPipeline{BucketsPath: "sales"},
				Script:              "MovingFunctions.sum(values)",
			}}}),
			want: "window must be a positive integer",
		},
		{
			name: "moving_fn rejects other scripts",
			aggs: histogramAggs(map[string]meta.Aggregations{"result": {MovingFn: &meta.AggregationMovingFn{
				AggregationPipeline: meta.AggregationPipeline{BucketsPath: "sales"},
				Window:              2,
				Script:              "return 1",
			}}}),
			want: "doesn't support script",
		},
		{
			name: "moving_fn rejects unknown functions",
			aggs: histogramAggs(map[string]meta.Aggregations{"result": {MovingFn: &meta.AggregationMovingFn{
				AggregationPipeline: meta.AggregationPipeline{BucketsPath: "sales"},
				Window:              2,
				Script:              "MovingFunctions.median(values)",
			}}}),
			want: "doesn't support MovingFunctions.median",
		},
		{
			name: "serial_diff rejects a negative lag",
			aggs: histogramAggs(map[string]meta.Aggregations{"result": {SerialDiff: &meta.AggregationSerialDiff{
				AggregationPipeline: meta.AggregationPipeline{BucketsPath: "sales"},
				Lag:                 -1,
			}}}),
			want: "lag must not be negative",
		},
		{
			name: "bucket_script needs a valid script",
			aggs: histogramAggs(map[string]meta.Aggregations{"result": {BucketScript: &meta.AggregationBucketScript{
				BucketsPath: map[string]string{"a": "sales"},
				Script:      "params.a +",
			}}}),
			want: "script compile err",
		},
		{
			name: "bucket_script script must be a string",
			aggs: histogramAggs(map[string]meta.Aggregations{"result": {BucketScript: &meta.AggregationBucketScript{
				BucketsPath: map[string]string{"a": "sales"},
				Script:      42,
			}}}),
			want: "script must be a string",
		},
		{
			name: "bucket_sort rejects an unknown order",
			aggs: histogramAggs(map[string]meta.Aggregations{"result": {BucketSort: &meta.AggregationBucketSort{
				Sort: []map[string]interface{}{{"sales": "up"}},
			}}}),
			want: "must be asc or desc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := salesResponse(f(10), f(30))
			resp["total_sales"] = meta.AggregationResponse{Value: float64(40)}
			aggs := tt.aggs
			if _, ok := aggs["sales_per_month"]; !ok {
				for name, agg := range histogramAggs(nil) {
					aggs[name] = agg
				}
			}
			err := ApplyPipelines(resp, aggs)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}
