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
	"math"
	"strconv"
	"strings"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

// DefaultPercents is the percents list elasticsearch uses when the
// percentiles aggregation doesn't specify one.
var DefaultPercents = []float64{1, 5, 25, 50, 75, 95, 99}

// StatsAggregation implements the elasticsearch stats / extended_stats
// aggregations over a numeric field.
type StatsAggregation struct {
	src      search.NumericValuesSource
	extended bool
	sigma    float64
}

// NewStatsAggregation returns the aggregation backing [stats].
func NewStatsAggregation(src search.NumericValuesSource) *StatsAggregation {
	return &StatsAggregation{src: src}
}

// NewExtendedStatsAggregation returns the aggregation backing [extended_stats].
// sigma is the std_deviation_bounds multiplier.
func NewExtendedStatsAggregation(src search.NumericValuesSource, sigma float64) *StatsAggregation {
	return &StatsAggregation{src: src, extended: true, sigma: sigma}
}

func (a *StatsAggregation) Fields() []string {
	return a.src.Fields()
}

func (a *StatsAggregation) Calculator() search.Calculator {
	return &StatsCalculator{
		src:      a.src,
		extended: a.extended,
		sigma:    a.sigma,
		min:      math.Inf(1),
		max:      math.Inf(-1),
	}
}

// StatsCalculator accumulates count, sum, min, max and sum of squares so that
// every stats/extended_stats metric can be derived and shards can be merged.
//
// It deliberately does not implement search.MetricCalculator: it has no single
// value, the response layer type-switches on the concrete type instead.
type StatsCalculator struct {
	src search.NumericValuesSource

	count        uint64
	sum          float64
	min          float64
	max          float64
	sumOfSquares float64

	extended bool
	sigma    float64
}

func (c *StatsCalculator) Consume(d *search.DocumentMatch) {
	for _, val := range c.src.Numbers(d) {
		c.count++
		c.sum += val
		c.sumOfSquares += val * val
		if val < c.min {
			c.min = val
		}
		if val > c.max {
			c.max = val
		}
	}
}

func (c *StatsCalculator) Merge(other search.Calculator) {
	o, ok := other.(*StatsCalculator)
	if !ok {
		return
	}
	c.count += o.count
	c.sum += o.sum
	c.sumOfSquares += o.sumOfSquares
	if o.min < c.min {
		c.min = o.min
	}
	if o.max > c.max {
		c.max = o.max
	}
}

func (c *StatsCalculator) Finish() {}

// Extended reports whether this calculator was built for [extended_stats].
func (c *StatsCalculator) Extended() bool { return c.extended }

// Sigma is the std_deviation_bounds multiplier.
func (c *StatsCalculator) Sigma() float64 { return c.sigma }

func (c *StatsCalculator) Count() uint64 { return c.count }

func (c *StatsCalculator) Sum() float64 { return c.sum }

// Min returns 0 when no value was collected. Elasticsearch returns null there,
// zinc emits 0 to keep the response type a plain float.
func (c *StatsCalculator) Min() float64 {
	if c.count == 0 {
		return 0
	}
	return c.min
}

// Max returns 0 when no value was collected, see Min.
func (c *StatsCalculator) Max() float64 {
	if c.count == 0 {
		return 0
	}
	return c.max
}

// Avg returns 0 when no value was collected, see Min.
func (c *StatsCalculator) Avg() float64 {
	if c.count == 0 {
		return 0
	}
	return c.sum / float64(c.count)
}

func (c *StatsCalculator) SumOfSquares() float64 { return c.sumOfSquares }

// Variance is the population variance, as elasticsearch reports it.
func (c *StatsCalculator) Variance() float64 {
	if c.count == 0 {
		return 0
	}
	avg := c.Avg()
	v := c.sumOfSquares/float64(c.count) - avg*avg
	if v < 0 {
		return 0 // floating point noise on a constant series
	}
	return v
}

func (c *StatsCalculator) StdDeviation() float64 {
	return math.Sqrt(c.Variance())
}

// ValueCountAggregation counts the number of field values, not the number of
// matching documents, as elasticsearch value_count does.
type ValueCountAggregation struct {
	src     search.FieldSource
	srcType int
}

// NewValueCountAggregation returns the aggregation backing [value_count].
// valueType selects how values are extracted, see the *ValueSource constants.
func NewValueCountAggregation(src search.FieldSource, valueType int) *ValueCountAggregation {
	return &ValueCountAggregation{src: src, srcType: valueType}
}

func (a *ValueCountAggregation) Fields() []string {
	return a.src.Fields()
}

func (a *ValueCountAggregation) Calculator() search.Calculator {
	return &ValueCountCalculator{src: a.src, srcType: a.srcType}
}

// ValueCountCalculator implements search.MetricCalculator so the generic
// metric response path renders it as {"value": n}.
type ValueCountCalculator struct {
	src     search.FieldSource
	srcType int
	count   uint64
}

func (c *ValueCountCalculator) Consume(d *search.DocumentMatch) {
	switch c.srcType {
	case NumericValueSource, NumericValuesSource, BooleanValueSource, BooleanValuesSource:
		c.count += uint64(len(c.src.Numbers(d)))
	default:
		c.count += uint64(len(c.src.Values(d)))
	}
}

func (c *ValueCountCalculator) Merge(other search.Calculator) {
	if o, ok := other.(*ValueCountCalculator); ok {
		c.count += o.count
	}
}

func (c *ValueCountCalculator) Finish() {}

func (c *ValueCountCalculator) Value() float64 { return float64(c.count) }

// PercentilesAggregation wraps riot's quantiles metric so that the requested
// percents travel with the calculator into the response layer.
type PercentilesAggregation struct {
	inner    *aggregations.QuantilesMetric
	percents []float64
}

// NewPercentilesAggregation returns the aggregation backing [percentiles].
func NewPercentilesAggregation(src search.NumericValuesSource, percents []float64) *PercentilesAggregation {
	if len(percents) == 0 {
		percents = DefaultPercents
	}
	return &PercentilesAggregation{
		inner:    aggregations.Quantiles(src),
		percents: percents,
	}
}

func (a *PercentilesAggregation) Fields() []string {
	return a.inner.Fields()
}

func (a *PercentilesAggregation) Calculator() search.Calculator {
	return &PercentilesCalculator{
		QuantilesCalculator: a.inner.Calculator().(*aggregations.QuantilesCalculator),
		percents:            a.percents,
	}
}

// PercentilesCalculator adds the percents list to riot's quantiles calculator.
// It does not implement search.MetricCalculator, the response layer
// type-switches on the concrete type.
type PercentilesCalculator struct {
	*aggregations.QuantilesCalculator
	percents []float64
}

// Merge unwraps the other calculator, the embedded riot implementation only
// accepts its own concrete type.
func (c *PercentilesCalculator) Merge(other search.Calculator) {
	if o, ok := other.(*PercentilesCalculator); ok {
		c.QuantilesCalculator.Merge(o.QuantilesCalculator)
		return
	}
	c.QuantilesCalculator.Merge(other)
}

func (c *PercentilesCalculator) Percents() []float64 { return c.percents }

// percentileKey renders the map key of a percentile or of a percentile rank
// the way elasticsearch does: the shortest representation that round trips,
// with at least one decimal, e.g "1.0", "99.99", "500.0". Rounding to a fixed
// number of decimals would report a value under the wrong key and let two
// requested percents collide into one.
func percentileKey(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// Values returns the percentile values keyed like elasticsearch does, e.g "99.0".
// An empty t-digest yields NaN, reported as 0.
func (c *PercentilesCalculator) Values() map[string]float64 {
	rv := make(map[string]float64, len(c.percents))
	for _, p := range c.percents {
		v, err := c.Quantile(p / 100)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			v = 0
		}
		rv[percentileKey(p)] = v
	}
	return rv
}
