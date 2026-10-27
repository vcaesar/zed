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

	"github.com/caio/go-tdigest/v5"
	"github.com/vcaesar/riot/search"
)

// percentileRanksCompression matches the t-digest compression riot uses for
// the [percentiles] aggregation.
const percentileRanksCompression = 100

// PercentileRanksAggregation implements the elasticsearch [percentile_ranks]
// aggregation: the percentage of the observed values that are below each of
// the requested values.
//
// It keeps its own t-digest rather than wrapping riot's quantiles metric,
// which exposes the quantile function but not its inverse.
type PercentileRanksAggregation struct {
	src    search.NumericValuesSource
	values []float64
}

// NewPercentileRanksAggregation returns the aggregation backing
// [percentile_ranks].
func NewPercentileRanksAggregation(src search.NumericValuesSource, values []float64) *PercentileRanksAggregation {
	return &PercentileRanksAggregation{src: src, values: values}
}

func (a *PercentileRanksAggregation) Fields() []string {
	return a.src.Fields()
}

func (a *PercentileRanksAggregation) Calculator() search.Calculator {
	rv := &PercentileRanksCalculator{src: a.src, values: a.values}
	rv.tdigest, rv.err = tdigest.New(tdigest.Compression(percentileRanksCompression))
	return rv
}

// PercentileRanksCalculator accumulates the field values into a t-digest and
// reads the ranks back through its CDF. It does not implement
// search.MetricCalculator, the response layer type-switches on the concrete
// type.
type PercentileRanksCalculator struct {
	src     search.NumericValuesSource
	values  []float64
	tdigest *tdigest.TDigest
	count   uint64
	err     error
}

func (c *PercentileRanksCalculator) Consume(d *search.DocumentMatch) {
	if c.tdigest == nil {
		return
	}
	for _, val := range c.src.Numbers(d) {
		if err := c.tdigest.Add(val); err != nil {
			if c.err == nil {
				c.err = err
			}
			continue
		}
		c.count++
	}
}

func (c *PercentileRanksCalculator) Merge(other search.Calculator) {
	o, ok := other.(*PercentileRanksCalculator)
	if !ok || c.tdigest == nil || o.tdigest == nil {
		return
	}
	if err := c.tdigest.Merge(o.tdigest); err != nil {
		if c.err == nil {
			c.err = err
		}
		return
	}
	c.count += o.count
	if c.err == nil {
		c.err = o.err
	}
}

func (c *PercentileRanksCalculator) Finish() {}

// Err reports the first t-digest error seen, if any.
func (c *PercentileRanksCalculator) Err() error { return c.err }

// Values returns the ranks keyed like elasticsearch does, e.g "500.0".
// Without any observation every rank is 0.
func (c *PercentileRanksCalculator) Values() map[string]float64 {
	rv := make(map[string]float64, len(c.values))
	for _, v := range c.values {
		var rank float64
		if c.count > 0 {
			rank = c.tdigest.CDF(v) * 100
			if math.IsNaN(rank) || math.IsInf(rank, 0) {
				rank = 0
			}
		}
		rv[percentileKey(v)] = rank
	}
	return rv
}
