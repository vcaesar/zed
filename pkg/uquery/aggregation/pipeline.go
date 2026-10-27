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
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"github.com/zincsearch/zincsearch/pkg/errors"
	"github.com/zincsearch/zincsearch/pkg/meta"
)

// gap_policy values, see
// https://www.elastic.co/guide/en/elasticsearch/reference/current/search-aggregations-pipeline.html
const (
	gapSkip        = "skip" // default: buckets without a value are ignored
	gapInsertZeros = "insert_zeros"
	gapKeepValues  = "keep_values"
)

// ApplyPipelines evaluates the pipeline aggregations of aggs over the response
// tree rendered by Response. Pipeline aggregations do not see documents, they
// only read the values other aggregations already produced, so they run after
// the search instead of during collection.
//
// Sibling pipelines (avg_bucket, sum_bucket, min_bucket, max_bucket,
// stats_bucket) are declared next to the multi bucket aggregation they read
// and add one value at that level. Parent pipelines (cumulative_sum,
// derivative, serial_diff, moving_fn, bucket_script, bucket_selector,
// bucket_sort) are declared inside a multi bucket aggregation and add one
// value per bucket, or drop and reorder the buckets.
func ApplyPipelines(resp map[string]meta.AggregationResponse, aggs map[string]meta.Aggregations) error {
	// depth first: the deepest levels must be complete before a pipeline of
	// an enclosing level reads them.
	for _, name := range sortedNames(aggs) {
		agg := aggs[name]
		if len(agg.Aggregations) == 0 {
			continue
		}
		r, ok := resp[name]
		if !ok {
			continue
		}

		if buckets := bucketList(r); buckets != nil {
			for _, bucket := range buckets {
				if err := applyInBucket(bucket, agg.Aggregations); err != nil {
					return err
				}
			}
			buckets, err := applyParents(buckets, agg.Aggregations)
			if err != nil {
				return err
			}
			r.Buckets = buckets
			resp[name] = r
			continue
		}
		if r.Aggregations != nil { // single bucket aggregation: filter, nested
			if err := ApplyPipelines(r.Aggregations, agg.Aggregations); err != nil {
				return err
			}
			continue
		}
		// keyed buckets ([filters]): parent pipelines need an order, skip them
		if keyed, ok := r.Buckets.(map[string]interface{}); ok {
			for _, v := range keyed {
				bucket, ok := v.(meta.AggregationResponse)
				if !ok || bucket.Aggregations == nil {
					continue
				}
				if err := ApplyPipelines(bucket.Aggregations, agg.Aggregations); err != nil {
					return err
				}
			}
		}
	}

	return applySiblings(resp, aggs)
}

func applyInBucket(bucket map[string]interface{}, aggs map[string]meta.Aggregations) error {
	sub := bucketAggs(bucket)
	if err := ApplyPipelines(sub, aggs); err != nil {
		return err
	}
	for name, r := range sub {
		bucket[name] = r
	}
	return nil
}

// applySiblings evaluates the sibling pipelines declared at this level.
func applySiblings(resp map[string]meta.AggregationResponse, aggs map[string]meta.Aggregations) error {
	for _, name := range sortedNames(aggs) {
		agg := aggs[name]
		kind, p := siblingPipeline(agg)
		if p == nil {
			continue
		}
		values, err := siblingValues(resp, kind, p)
		if err != nil {
			return err
		}
		resp[name] = siblingResponse(kind, values)
	}
	return nil
}

func siblingPipeline(agg meta.Aggregations) (string, *meta.AggregationPipeline) {
	switch {
	case agg.AvgBucket != nil:
		return "avg_bucket", agg.AvgBucket
	case agg.SumBucket != nil:
		return "sum_bucket", agg.SumBucket
	case agg.MinBucket != nil:
		return "min_bucket", agg.MinBucket
	case agg.MaxBucket != nil:
		return "max_bucket", agg.MaxBucket
	case agg.StatsBucket != nil:
		return "stats_bucket", agg.StatsBucket
	default:
		return "", nil
	}
}

// siblingValues collects the series a sibling pipeline reduces.
func siblingValues(resp map[string]meta.AggregationResponse, kind string, p *meta.AggregationPipeline) ([]float64, error) {
	buckets, path, err := findBuckets(resp, kind, p.BucketsPath)
	if err != nil {
		return nil, err
	}
	gap, err := gapPolicy(kind, p.GapPolicy)
	if err != nil {
		return nil, err
	}

	values := make([]float64, 0, len(buckets))
	for _, bucket := range buckets {
		v, ok := bucketValue(bucket, path)
		if !ok {
			if gap != gapInsertZeros {
				continue
			}
			v = 0
		}
		values = append(values, v)
	}
	return values, nil
}

func siblingResponse(kind string, values []float64) meta.AggregationResponse {
	switch kind {
	case "sum_bucket":
		var sum float64
		for _, v := range values {
			sum += v
		}
		return meta.AggregationResponse{Value: sum}
	case "avg_bucket":
		if len(values) == 0 {
			return meta.AggregationResponse{}
		}
		var sum float64
		for _, v := range values {
			sum += v
		}
		return meta.AggregationResponse{Value: sum / float64(len(values))}
	case "min_bucket":
		if len(values) == 0 {
			return meta.AggregationResponse{}
		}
		min := values[0]
		for _, v := range values {
			if v < min {
				min = v
			}
		}
		return meta.AggregationResponse{Value: min}
	case "max_bucket":
		if len(values) == 0 {
			return meta.AggregationResponse{}
		}
		max := values[0]
		for _, v := range values {
			if v > max {
				max = v
			}
		}
		return meta.AggregationResponse{Value: max}
	default: // stats_bucket
		count := uint64(len(values))
		var sum float64
		rv := meta.AggregationResponse{Count: &count, Sum: &sum}
		if count == 0 {
			// elasticsearch reports null for min / max / avg of an empty
			// series, only count and sum are numbers.
			return rv
		}
		min, max := values[0], values[0]
		for _, v := range values {
			sum += v
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
		avg := sum / float64(count)
		rv.Min, rv.Max, rv.Avg = &min, &max, &avg
		return rv
	}
}

// applyParents evaluates the parent pipelines declared inside a multi bucket
// aggregation, over its buckets. Value producing pipelines run first, then
// bucket_selector drops buckets and bucket_sort reorders them, as in
// elasticsearch.
func applyParents(buckets []map[string]interface{}, aggs map[string]meta.Aggregations) ([]map[string]interface{}, error) {
	for _, name := range sortedNames(aggs) {
		if err := applyParent(buckets, name, aggs[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range sortedNames(aggs) {
		if p := aggs[name].BucketSelector; p != nil {
			var err error
			if buckets, err = applyBucketSelector(buckets, p); err != nil {
				return nil, err
			}
		}
	}
	for _, name := range sortedNames(aggs) {
		if p := aggs[name].BucketSort; p != nil {
			var err error
			if buckets, err = applyBucketSort(buckets, p); err != nil {
				return nil, err
			}
		}
	}
	return buckets, nil
}

func applyParent(buckets []map[string]interface{}, name string, agg meta.Aggregations) error {
	switch {
	case agg.CumulativeSum != nil:
		return applySeries(buckets, name, "cumulative_sum", agg.CumulativeSum, cumulativeSum)
	case agg.Derivative != nil:
		return applySeries(buckets, name, "derivative", agg.Derivative, derivative)
	case agg.SerialDiff != nil:
		lag := agg.SerialDiff.Lag
		if lag == 0 {
			lag = 1
		}
		if lag < 0 {
			return errors.New(errors.ErrorTypeParsingException, "[serial_diff] aggregation lag must not be negative")
		}
		return applySeries(buckets, name, "serial_diff", &agg.SerialDiff.AggregationPipeline,
			func(values []float64) []float64 { return serialDiff(values, lag) })
	case agg.MovingFn != nil:
		fn, err := movingFunction(agg.MovingFn.Script)
		if err != nil {
			return err
		}
		window := agg.MovingFn.Window
		if window <= 0 {
			return errors.New(errors.ErrorTypeParsingException, "[moving_fn] aggregation window must be a positive integer")
		}
		shift := agg.MovingFn.Shift
		return applySeries(buckets, name, "moving_fn", &agg.MovingFn.AggregationPipeline,
			func(values []float64) []float64 { return movingFn(values, window, shift, fn) })
	case agg.BucketScript != nil:
		return applyBucketScript(buckets, name, agg.BucketScript)
	default:
		return nil
	}
}

// applySeries reads one value per bucket, reduces the series and writes the
// results back. reduce returns NaN for the buckets it has no value for, for
// example the first bucket of a derivative.
func applySeries(
	buckets []map[string]interface{},
	name, kind string,
	p *meta.AggregationPipeline,
	reduce func([]float64) []float64,
) error {
	gap, err := gapPolicy(kind, p.GapPolicy)
	if err != nil {
		return err
	}
	if p.BucketsPath == "" {
		return errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] aggregation needs buckets_path", kind))
	}

	index := make([]int, 0, len(buckets))
	values := make([]float64, 0, len(buckets))
	var gaps []int
	for i, bucket := range buckets {
		v, ok := bucketValue(bucket, p.BucketsPath)
		if !ok {
			if gap != gapInsertZeros {
				if gap == gapKeepValues {
					gaps = append(gaps, i)
				}
				continue
			}
			v = 0
		}
		index = append(index, i)
		values = append(values, v)
	}

	for i, v := range reduce(values) {
		setPipelineValue(buckets[index[i]], name, v, gap)
	}
	for _, i := range gaps {
		setPipelineValue(buckets[i], name, math.NaN(), gap)
	}
	return nil
}

// setPipelineValue writes a parent pipeline result into a bucket. A missing
// value is omitted, unless keep_values asked for an explicit null. A non
// finite value (a bucket_script dividing by zero, for example) has no json
// representation and is treated as missing.
func setPipelineValue(bucket map[string]interface{}, name string, v float64, gap string) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		if gap == gapKeepValues {
			bucket[name] = meta.AggregationResponse{}
		}
		return
	}
	bucket[name] = meta.AggregationResponse{Value: v}
}

func cumulativeSum(values []float64) []float64 {
	rv := make([]float64, len(values))
	var sum float64
	for i, v := range values {
		sum += v
		rv[i] = sum
	}
	return rv
}

func derivative(values []float64) []float64 {
	return serialDiff(values, 1)
}

func serialDiff(values []float64, lag int) []float64 {
	rv := make([]float64, len(values))
	for i := range values {
		if i < lag {
			rv[i] = math.NaN() // elasticsearch omits the first lag buckets
			continue
		}
		rv[i] = values[i] - values[i-lag]
	}
	return rv
}

// movingFn evaluates fn over a sliding window of the previous window values.
// shift moves the window towards the current bucket; the default 0 excludes
// the current bucket, as elasticsearch does.
func movingFn(values []float64, window, shift int, fn func([]float64) float64) []float64 {
	rv := make([]float64, len(values))
	for i := range values {
		start := i - window + shift
		start = min(max(start, 0), len(values))
		end := i + shift
		if end > len(values) {
			end = len(values)
		}
		if end < start {
			end = start
		}
		rv[i] = fn(values[start:end])
	}
	return rv
}

// movingFnScript matches the painless calls elasticsearch documents for
// moving_fn, the only scripts zinc supports.
var movingFnScript = regexp.MustCompile(`^MovingFunctions\.(\w+)\(\s*values\s*(?:,(.*))?\)$`)

func movingFunction(script string) (func([]float64) float64, error) {
	m := movingFnScript.FindStringSubmatch(strings.TrimSpace(script))
	if m == nil {
		return nil, errors.New(errors.ErrorTypeNotImplemented,
			fmt.Sprintf("[moving_fn] aggregation doesn't support script [%s], use MovingFunctions.<fn>(values)", script))
	}
	switch m[1] {
	case "min":
		return func(values []float64) float64 { return reduceWindow(values, math.Min) }, nil
	case "max":
		return func(values []float64) float64 { return reduceWindow(values, math.Max) }, nil
	case "sum":
		return windowSum, nil
	case "unweightedAvg":
		return unweightedAvg, nil
	case "linearWeightedAvg":
		return linearWeightedAvg, nil
	case "stdDev":
		if arg := strings.TrimSpace(m[2]); arg != "MovingFunctions.unweightedAvg(values)" {
			return nil, errors.New(errors.ErrorTypeNotImplemented,
				fmt.Sprintf("[moving_fn] aggregation stdDev only supports MovingFunctions.unweightedAvg(values) as average, got [%s]", arg))
		}
		return stdDev, nil
	default:
		return nil, errors.New(errors.ErrorTypeNotImplemented,
			fmt.Sprintf("[moving_fn] aggregation doesn't support MovingFunctions.%s", m[1]))
	}
}

func reduceWindow(values []float64, f func(a, b float64) float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	rv := values[0]
	for _, v := range values[1:] {
		rv = f(rv, v)
	}
	return rv
}

// windowSum returns 0 for an empty window, as MovingFunctions.sum does.
func windowSum(values []float64) float64 {
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum
}

func unweightedAvg(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	return windowSum(values) / float64(len(values))
}

func linearWeightedAvg(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	var sum, weights float64
	for i, v := range values {
		weight := float64(i + 1)
		sum += v * weight
		weights += weight
	}
	return sum / weights
}

func stdDev(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	avg := unweightedAvg(values)
	var variance float64
	for _, v := range values {
		variance += (v - avg) * (v - avg)
	}
	return math.Sqrt(variance / float64(len(values)))
}

func applyBucketScript(buckets []map[string]interface{}, name string, p *meta.AggregationBucketScript) error {
	program, gap, err := compileBucketScript("bucket_script", p)
	if err != nil {
		return err
	}
	for _, bucket := range buckets {
		params, ok := scriptParams(bucket, p.BucketsPath, gap)
		if !ok {
			setPipelineValue(bucket, name, math.NaN(), gap)
			continue
		}
		out, err := runBucketScript("bucket_script", program, params)
		if err != nil {
			return err
		}
		v, err := toFloat(out)
		if err != nil {
			return errors.New(errors.ErrorTypeParsingException,
				fmt.Sprintf("[bucket_script] aggregation script must return a number, got %T", out))
		}
		setPipelineValue(bucket, name, v, gap)
	}
	return nil
}

func applyBucketSelector(buckets []map[string]interface{}, p *meta.AggregationBucketScript) ([]map[string]interface{}, error) {
	program, gap, err := compileBucketScript("bucket_selector", p)
	if err != nil {
		return nil, err
	}
	rv := make([]map[string]interface{}, 0, len(buckets))
	for _, bucket := range buckets {
		params, ok := scriptParams(bucket, p.BucketsPath, gap)
		if !ok {
			continue // elasticsearch drops the bucket when a path is missing
		}
		out, err := runBucketScript("bucket_selector", program, params)
		if err != nil {
			return nil, err
		}
		keep, err := truthy(out)
		if err != nil {
			return nil, errors.New(errors.ErrorTypeParsingException,
				fmt.Sprintf("[bucket_selector] aggregation script must return a boolean, got %T", out))
		}
		if keep {
			rv = append(rv, bucket)
		}
	}
	return rv, nil
}

func compileBucketScript(kind string, p *meta.AggregationBucketScript) (*vm.Program, string, error) {
	gap, err := gapPolicy(kind, p.GapPolicy)
	if err != nil {
		return nil, "", err
	}
	if len(p.BucketsPath) == 0 {
		return nil, "", errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] aggregation needs buckets_path", kind))
	}
	source, err := scriptSource(kind, p.Script)
	if err != nil {
		return nil, "", err
	}
	env := map[string]interface{}{"params": map[string]float64{}}
	program, err := expr.Compile(source, expr.Env(env))
	if err != nil {
		return nil, "", errors.New(errors.ErrorTypeParsingException,
			fmt.Sprintf("[%s] aggregation script compile err %s", kind, err.Error()))
	}
	return program, gap, nil
}

func runBucketScript(kind string, program *vm.Program, params map[string]float64) (interface{}, error) {
	out, err := expr.Run(program, map[string]interface{}{"params": params})
	if err != nil {
		return nil, errors.New(errors.ErrorTypeRuntimeException,
			fmt.Sprintf("[%s] aggregation script run err %s", kind, err.Error()))
	}
	return out, nil
}

// scriptParams resolves the buckets_path of a script pipeline in one bucket.
// ok is false when a path has no value and the gap policy does not fill it.
func scriptParams(bucket map[string]interface{}, paths map[string]string, gap string) (map[string]float64, bool) {
	params := make(map[string]float64, len(paths))
	for name, path := range paths {
		v, ok := bucketValue(bucket, path)
		if !ok {
			if gap != gapInsertZeros {
				return nil, false
			}
			v = 0
		}
		params[name] = v
	}
	return params, true
}

// scriptSource accepts both the short form "a + b" and {"source": "a + b"}.
func scriptSource(kind string, script interface{}) (string, error) {
	switch v := script.(type) {
	case string:
		return v, nil
	case map[string]interface{}:
		if source, ok := v["source"].(string); ok {
			return source, nil
		}
	}
	return "", errors.New(errors.ErrorTypeParsingException,
		fmt.Sprintf(`[%s] aggregation script must be a string or {"source": "..."}`, kind))
}

func truthy(v interface{}) (bool, error) {
	if b, ok := v.(bool); ok {
		return b, nil
	}
	f, err := toFloat(v)
	if err != nil {
		return false, err
	}
	return f != 0, nil
}

func applyBucketSort(buckets []map[string]interface{}, p *meta.AggregationBucketSort) ([]map[string]interface{}, error) {
	gap, err := gapPolicy("bucket_sort", p.GapPolicy)
	if err != nil {
		return nil, err
	}
	if p.From < 0 || p.Size < 0 {
		return nil, errors.New(errors.ErrorTypeParsingException, "[bucket_sort] aggregation from and size must not be negative")
	}

	type sortKey struct {
		path string
		asc  bool
	}
	keys := make([]sortKey, 0, len(p.Sort))
	for _, entry := range p.Sort {
		for path, order := range entry {
			asc, err := sortOrder(order)
			if err != nil {
				return nil, err
			}
			keys = append(keys, sortKey{path: path, asc: asc})
		}
	}

	value := func(bucket map[string]interface{}, path string) float64 {
		v, ok := bucketValue(bucket, path)
		if !ok {
			if gap == gapInsertZeros {
				return 0
			}
			return math.NaN() // missing values sort last
		}
		return v
	}
	sort.SliceStable(buckets, func(i, j int) bool {
		for _, key := range keys {
			a, b := value(buckets[i], key.path), value(buckets[j], key.path)
			switch {
			case math.IsNaN(a) && math.IsNaN(b):
				continue
			case math.IsNaN(a):
				return false
			case math.IsNaN(b):
				return true
			case a == b:
				continue
			case key.asc:
				return a < b
			default:
				return a > b
			}
		}
		return false
	})

	if p.From >= len(buckets) {
		return []map[string]interface{}{}, nil
	}
	buckets = buckets[p.From:]
	if p.Size > 0 && p.Size < len(buckets) {
		buckets = buckets[:p.Size]
	}
	return buckets, nil
}

// sortOrder reads "asc" / "desc" from either "desc" or {"order": "desc"}.
func sortOrder(order interface{}) (bool, error) {
	var v string
	switch order := order.(type) {
	case string:
		v = order
	case map[string]interface{}:
		s, ok := order["order"].(string)
		if !ok {
			return false, errors.New(errors.ErrorTypeParsingException, "[bucket_sort] aggregation sort order must be asc or desc")
		}
		v = s
	default:
		return false, errors.New(errors.ErrorTypeParsingException, "[bucket_sort] aggregation sort must be a string or an object")
	}
	switch v {
	case "asc":
		return true, nil
	case "desc":
		return false, nil
	default:
		return false, errors.New(errors.ErrorTypeParsingException,
			fmt.Sprintf("[bucket_sort] aggregation sort order must be asc or desc, got [%s]", v))
	}
}

func gapPolicy(kind, policy string) (string, error) {
	switch policy {
	case "":
		return gapSkip, nil
	case gapSkip, gapInsertZeros, gapKeepValues:
		return policy, nil
	default:
		return "", errors.New(errors.ErrorTypeParsingException,
			fmt.Sprintf("[%s] aggregation gap_policy must be skip, insert_zeros or keep_values, got [%s]", kind, policy))
	}
}

// findBuckets walks a buckets_path until it reaches a multi bucket
// aggregation, and returns it with the remaining path.
func findBuckets(resp map[string]meta.AggregationResponse, kind, path string) ([]map[string]interface{}, string, error) {
	name, rest, _ := strings.Cut(path, ">")
	r, ok := resp[name]
	if !ok {
		return nil, "", errors.New(errors.ErrorTypeParsingException,
			fmt.Sprintf("[%s] aggregation buckets_path [%s] not found", kind, path))
	}
	if buckets := bucketList(r); buckets != nil {
		return buckets, rest, nil
	}
	if r.Aggregations != nil && rest != "" {
		return findBuckets(r.Aggregations, kind, rest)
	}
	return nil, "", errors.New(errors.ErrorTypeParsingException,
		fmt.Sprintf("[%s] aggregation buckets_path [%s] is not a multi bucket aggregation", kind, path))
}

// bucketList returns the buckets of a multi bucket aggregation response, nil
// for anything else including the keyed buckets of [filters].
func bucketList(r meta.AggregationResponse) []map[string]interface{} {
	buckets, _ := r.Buckets.([]map[string]interface{})
	return buckets
}

// bucketAggs returns the sub aggregation responses stored in a bucket.
func bucketAggs(bucket map[string]interface{}) map[string]meta.AggregationResponse {
	rv := make(map[string]meta.AggregationResponse, len(bucket))
	for name, v := range bucket {
		if r, ok := v.(meta.AggregationResponse); ok {
			rv[name] = r
		}
	}
	return rv
}

// bucketValue resolves a buckets_path inside one bucket: _count, _key, or a
// path of sub aggregation names ending in an optional metric, for example
// "sales>total.avg".
func bucketValue(bucket map[string]interface{}, path string) (float64, bool) {
	switch path {
	case "", "_count":
		v, err := toFloat(bucket["doc_count"])
		return v, err == nil
	case "_key":
		v, err := toFloat(bucket["key"])
		return v, err == nil
	}
	return aggValue(bucketAggs(bucket), path)
}

func aggValue(aggs map[string]meta.AggregationResponse, path string) (float64, bool) {
	name, rest, _ := strings.Cut(path, ">")
	if rest != "" {
		r, ok := aggs[name]
		if !ok || r.Aggregations == nil {
			return 0, false
		}
		return aggValue(r.Aggregations, rest)
	}

	metric := ""
	if _, ok := aggs[name]; !ok {
		if i := strings.LastIndex(name, "."); i > 0 {
			name, metric = name[:i], name[i+1:]
		}
	}
	r, ok := aggs[name]
	if !ok {
		return 0, false
	}
	return metricValue(r, metric)
}

func metricValue(r meta.AggregationResponse, metric string) (float64, bool) {
	switch metric {
	case "count":
		if r.Count != nil {
			return float64(*r.Count), true
		}
	case "min":
		if r.Min != nil {
			return *r.Min, true
		}
	case "max":
		if r.Max != nil {
			return *r.Max, true
		}
	case "avg":
		if r.Avg != nil {
			return *r.Avg, true
		}
	case "sum":
		if r.Sum != nil {
			return *r.Sum, true
		}
	case "doc_count":
		if r.DocCount != nil {
			return float64(*r.DocCount), true
		}
	default: // "" or "value"
		if r.Value != nil {
			v, err := toFloat(r.Value)
			return v, err == nil
		}
		if r.DocCount != nil { // single bucket aggregation
			return float64(*r.DocCount), true
		}
	}
	return 0, false
}

func sortedNames(aggs map[string]meta.Aggregations) []string {
	names := make([]string, 0, len(aggs))
	for name := range aggs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// toFloat converts the numeric types that reach the response tree through
// interface{} values.
func toFloat(v interface{}) (float64, error) {
	switch v := v.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("not a number: %s", v)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("not a number: %T", v)
	}
}
