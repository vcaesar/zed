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
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

// FilterPredicate decides whether a document belongs to a bucket.
//
// Calculators only see a search.DocumentMatch, so the query of a [filter] /
// [filters] aggregation is compiled into a doc-value predicate by the uquery
// layer. Fields lists the doc-value fields the predicate reads; they are
// reported through Aggregation.Fields so the collector loads them.
type FilterPredicate struct {
	Fields []string
	Match  func(*search.DocumentMatch) bool
}

// FilterAggregation implements the elasticsearch single bucket aggregations
// that select a subset of the matching documents: [filter] and [nested].
type FilterAggregation struct {
	pred         *FilterPredicate
	aggregations map[string]search.Aggregation
}

// NewFilterAggregation returns the aggregation backing [filter].
func NewFilterAggregation(pred *FilterPredicate) *FilterAggregation {
	rv := &FilterAggregation{
		pred:         pred,
		aggregations: map[string]search.Aggregation{"count": aggregations.CountMatches()},
	}
	return rv
}

func (a *FilterAggregation) Fields() []string {
	rv := append([]string{}, a.pred.Fields...)
	for _, agg := range a.aggregations {
		rv = append(rv, agg.Fields()...)
	}
	return rv
}

func (a *FilterAggregation) AddAggregation(name string, aggregation search.Aggregation) {
	a.aggregations[name] = aggregation
}

func (a *FilterAggregation) Calculator() search.Calculator {
	return &FilterCalculator{
		pred:   a.pred,
		bucket: search.NewBucket("filter", a.aggregations),
	}
}

// FilterCalculator holds the single bucket of a [filter] aggregation. It
// deliberately implements neither MetricCalculator nor BucketCalculator: the
// response layer renders doc_count plus the sub-aggregations inline.
type FilterCalculator struct {
	pred   *FilterPredicate
	bucket *search.Bucket
}

func (c *FilterCalculator) Consume(d *search.DocumentMatch) {
	if c.pred.Match(d) {
		c.bucket.Consume(d)
	}
}

func (c *FilterCalculator) Merge(other search.Calculator) {
	if o, ok := other.(*FilterCalculator); ok {
		c.bucket.Merge(o.bucket)
	}
}

func (c *FilterCalculator) Finish() { c.bucket.Finish() }

// Bucket returns the bucket holding the sub-aggregations.
func (c *FilterCalculator) Bucket() *search.Bucket { return c.bucket }

func (c *FilterCalculator) DocCount() uint64 { return c.bucket.Count() }
