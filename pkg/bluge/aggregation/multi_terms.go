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
	"sort"
	"strconv"
	"strings"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

// MultiTermsKeySeparator joins the per field terms of a composite key.
// A term containing the separator produces an ambiguous key_as_string,
// elasticsearch escapes it instead.
const MultiTermsKeySeparator = "|"

// MultiTermsAggregation implements the elasticsearch [multi_terms]
// aggregation: one bucket per combination of the values of several fields.
// Documents missing a value for any of the fields are skipped, as in
// elasticsearch.
type MultiTermsAggregation struct {
	srcs     []search.FieldSource
	srcTypes []int
	size     int

	aggregations map[string]search.Aggregation

	lessFunc func(a, b *search.Bucket) bool
	desc     bool
}

// NewMultiTermsAggregation returns the aggregation backing [multi_terms].
// valueTypes selects the value extraction per field, see the *ValueSource
// constants.
func NewMultiTermsAggregation(srcs []search.FieldSource, valueTypes []int, size int) *MultiTermsAggregation {
	return &MultiTermsAggregation{
		srcs:         srcs,
		srcTypes:     valueTypes,
		size:         size,
		aggregations: map[string]search.Aggregation{"count": aggregations.CountMatches()},
		lessFunc:     orderLessFunc(OrderByCount),
		desc:         true,
	}
}

// SetOrder configures the bucket order, see TermsAggregation.SetOrder.
func (a *MultiTermsAggregation) SetOrder(key string, asc bool) {
	a.lessFunc = orderLessFunc(key)
	a.desc = !asc
}

func (a *MultiTermsAggregation) Fields() []string {
	var rv []string
	for _, src := range a.srcs {
		rv = append(rv, src.Fields()...)
	}
	for _, agg := range a.aggregations {
		rv = append(rv, agg.Fields()...)
	}
	return rv
}

func (a *MultiTermsAggregation) AddAggregation(name string, aggregation search.Aggregation) {
	a.aggregations[name] = aggregation
}

func (a *MultiTermsAggregation) Calculator() search.Calculator {
	return &MultiTermsCalculator{
		srcs:         a.srcs,
		srcTypes:     a.srcTypes,
		size:         a.size,
		aggregations: a.aggregations,
		lessFunc:     a.lessFunc,
		desc:         a.desc,
		bucketsMap:   make(map[string]*search.Bucket),
		keys:         make(map[string][]string),
	}
}

// MultiTermsCalculator buckets documents by composite key, ordered by
// descending doc count unless another order was requested.
type MultiTermsCalculator struct {
	srcs     []search.FieldSource
	srcTypes []int
	size     int

	aggregations map[string]search.Aggregation

	lessFunc func(a, b *search.Bucket) bool
	desc     bool

	bucketsList []*search.Bucket
	bucketsMap  map[string]*search.Bucket
	keys        map[string][]string
}

// term renders the first value of one field, mirroring TermsCalculator.
func (a *MultiTermsCalculator) term(i int, d *search.DocumentMatch) (string, bool) {
	src := a.srcs[i]
	switch a.srcTypes[i] {
	case NumericValueSource, NumericValuesSource:
		nums := src.Numbers(d)
		if len(nums) == 0 {
			return "", false
		}
		return strconv.FormatFloat(nums[0], 'f', -1, 64), true
	case BooleanValueSource, BooleanValuesSource:
		nums := src.Numbers(d)
		if len(nums) == 0 {
			return "", false
		}
		if nums[0] != 0 {
			return "true", true
		}
		return "false", true
	default:
		term := src.Value(d)
		if len(term) == 0 {
			return "", false
		}
		return string(term), true
	}
}

func (a *MultiTermsCalculator) Consume(d *search.DocumentMatch) {
	key := make([]string, 0, len(a.srcs))
	for i := range a.srcs {
		term, ok := a.term(i, d)
		if !ok {
			return // elasticsearch skips documents missing one of the fields
		}
		key = append(key, term)
	}

	name := strings.Join(key, MultiTermsKeySeparator)
	if bucket, ok := a.bucketsMap[name]; ok {
		bucket.Consume(d)
		return
	}
	bucket := search.NewBucket(name, a.aggregations)
	bucket.Consume(d)
	a.bucketsMap[name] = bucket
	a.bucketsList = append(a.bucketsList, bucket)
	a.keys[name] = key
}

func (a *MultiTermsCalculator) Merge(other search.Calculator) {
	o, ok := other.(*MultiTermsCalculator)
	if !ok {
		return
	}
	for _, bucket := range o.bucketsList {
		if local, ok := a.bucketsMap[bucket.Name()]; ok {
			local.Merge(bucket)
			continue
		}
		a.bucketsMap[bucket.Name()] = bucket
		a.bucketsList = append(a.bucketsList, bucket)
		a.keys[bucket.Name()] = o.keys[bucket.Name()]
	}
	a.Finish()
}

func (a *MultiTermsCalculator) Finish() {
	for _, bucket := range a.bucketsList {
		bucket.Finish()
	}
	sort.SliceStable(a.bucketsList, func(i, j int) bool {
		if a.desc {
			return a.lessFunc(a.bucketsList[j], a.bucketsList[i])
		}
		return a.lessFunc(a.bucketsList[i], a.bucketsList[j])
	})
	if a.size > 0 && a.size < len(a.bucketsList) {
		a.bucketsList = a.bucketsList[:a.size]
	}
}

func (a *MultiTermsCalculator) Buckets() []*search.Bucket {
	return a.bucketsList
}

// Key returns the per field terms of a bucket.
func (a *MultiTermsCalculator) Key(name string) []string {
	return a.keys[name]
}
