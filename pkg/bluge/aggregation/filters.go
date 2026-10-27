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
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

// FiltersAggregation implements the elasticsearch [filters] aggregation: one
// named bucket per filter, plus an optional bucket for the documents that
// match none of them. As in elasticsearch a document may land in several
// buckets at once.
type FiltersAggregation struct {
	names        []string
	preds        []*FilterPredicate
	otherKey     string
	aggregations map[string]search.Aggregation
}

// NewFiltersAggregation returns the aggregation backing [filters].
// otherBucketKey is empty when the other bucket is not requested.
func NewFiltersAggregation(otherBucketKey string) *FiltersAggregation {
	return &FiltersAggregation{
		otherKey:     otherBucketKey,
		aggregations: map[string]search.Aggregation{"count": aggregations.CountMatches()},
	}
}

// AddFilter registers a named filter; buckets keep the insertion order.
func (a *FiltersAggregation) AddFilter(name string, pred *FilterPredicate) {
	a.names = append(a.names, name)
	a.preds = append(a.preds, pred)
}

func (a *FiltersAggregation) Fields() []string {
	var rv []string
	for _, pred := range a.preds {
		rv = append(rv, pred.Fields...)
	}
	for _, agg := range a.aggregations {
		rv = append(rv, agg.Fields()...)
	}
	return rv
}

func (a *FiltersAggregation) AddAggregation(name string, aggregation search.Aggregation) {
	a.aggregations[name] = aggregation
}

func (a *FiltersAggregation) Calculator() search.Calculator {
	rv := &FiltersCalculator{preds: a.preds, otherKey: a.otherKey}
	for _, name := range a.names {
		rv.buckets = append(rv.buckets, search.NewBucket(name, a.aggregations))
	}
	if a.otherKey != "" {
		rv.other = search.NewBucket(a.otherKey, a.aggregations)
	}
	return rv
}

// FiltersCalculator holds one bucket per named filter. It deliberately does
// not implement search.BucketCalculator: the response layer renders the
// buckets as a keyed object, not as an array.
type FiltersCalculator struct {
	preds    []*FilterPredicate
	buckets  []*search.Bucket
	other    *search.Bucket
	otherKey string
}

func (c *FiltersCalculator) Consume(d *search.DocumentMatch) {
	var matched bool
	for i, pred := range c.preds {
		if pred.Match(d) {
			c.buckets[i].Consume(d)
			matched = true
		}
	}
	if !matched && c.other != nil {
		c.other.Consume(d)
	}
}

func (c *FiltersCalculator) Merge(other search.Calculator) {
	o, ok := other.(*FiltersCalculator)
	if !ok {
		return
	}
	for i := range c.buckets {
		if i < len(o.buckets) {
			c.buckets[i].Merge(o.buckets[i])
		}
	}
	if c.other != nil && o.other != nil {
		c.other.Merge(o.other)
	}
}

func (c *FiltersCalculator) Finish() {
	for _, bucket := range c.buckets {
		bucket.Finish()
	}
	if c.other != nil {
		c.other.Finish()
	}
}

// NamedBuckets returns the filter buckets in request order, followed by the
// other bucket when it was requested.
func (c *FiltersCalculator) NamedBuckets() []*search.Bucket {
	if c.other == nil {
		return c.buckets
	}
	return append(append([]*search.Bucket{}, c.buckets...), c.other)
}
