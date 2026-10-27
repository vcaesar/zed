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

package meta

// AggregationFilters
// {"filters": {"filters": {"errors": {"match": {...}}, "warnings": {...}}, "other_bucket_key": "other"}}
// other_bucket adds a bucket for the documents matching no filter; setting
// other_bucket_key implies it, as in elasticsearch.
type AggregationFilters struct {
	Filters        map[string]interface{} `json:"filters"`
	OtherBucket    bool                   `json:"other_bucket,omitempty"`
	OtherBucketKey string                 `json:"other_bucket_key,omitempty"`
}

// DefaultOtherBucketKey is the key elasticsearch gives the other bucket of a
// [filters] aggregation when other_bucket_key is not set.
const DefaultOtherBucketKey = "_other_"

// AggregationMultiTerms
// {"multi_terms": {"terms": [{"field": "a"}, {"field": "b"}], "size": 10}}
type AggregationMultiTerms struct {
	Terms []AggregationMetric `json:"terms"`
	Size  int                 `json:"size"`
	Order map[string]string   `json:"order"`
}

// AggregationTopHits
// {"top_hits": {"size": 3, "sort": [...], "_source": [...]}}
type AggregationTopHits struct {
	Size   int         `json:"size"`
	From   int         `json:"from"`
	Sort   interface{} `json:"sort"`
	Source interface{} `json:"_source"`
}

// AggregationPercentileRanks
// {"percentile_ranks": {"field": "load", "values": [500, 600]}}
type AggregationPercentileRanks struct {
	Field  string    `json:"field"`
	Values []float64 `json:"values"`
	Keyed  bool      `json:"keyed"`
}

// AggregationNested
// {"nested": {"path": "comments"}} / {"reverse_nested": {}}
type AggregationNested struct {
	Path string `json:"path"`
}

// AggregationPipeline is the common shape of sibling/parent pipeline aggregations.
// {"avg_bucket": {"buckets_path": "sales_per_month>sales", "gap_policy": "skip", "format": "..."}}
type AggregationPipeline struct {
	BucketsPath string `json:"buckets_path"`
	GapPolicy   string `json:"gap_policy,omitempty"` // skip (default), insert_zeros, keep_values
	Format      string `json:"format,omitempty"`
}

// AggregationSerialDiff
// {"serial_diff": {"buckets_path": "the_sum", "lag": 7}}
type AggregationSerialDiff struct {
	AggregationPipeline
	Lag int `json:"lag"`
}

// AggregationMovingFn
// {"moving_fn": {"buckets_path": "the_sum", "window": 10, "script": "MovingFunctions.unweightedAvg(values)", "shift": 0}}
type AggregationMovingFn struct {
	AggregationPipeline
	Window int    `json:"window"`
	Script string `json:"script"`
	Shift  int    `json:"shift,omitempty"`
}

// AggregationBucketScript is shared by bucket_script and bucket_selector.
// {"bucket_script": {"buckets_path": {"t": "total", "c": "count"}, "script": "params.t / params.c"}}
// script accepts either a string or {"source": "..."}.
type AggregationBucketScript struct {
	BucketsPath map[string]string `json:"buckets_path"`
	Script      interface{}       `json:"script"`
	GapPolicy   string            `json:"gap_policy,omitempty"`
	Format      string            `json:"format,omitempty"`
}

// AggregationBucketSort
// {"bucket_sort": {"sort": [{"total_sales": {"order": "desc"}}], "size": 3, "from": 0}}
type AggregationBucketSort struct {
	Sort      []map[string]interface{} `json:"sort"`
	From      int                      `json:"from"`
	Size      int                      `json:"size"`
	GapPolicy string                   `json:"gap_policy,omitempty"`
}
