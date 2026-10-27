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
)

// stubMatchReader serves hand-built doc values, so calculators can be tested
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

func numTerm(v float64) []byte {
	return numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(v), 0)
}

func numTerms(vs ...float64) [][]byte {
	rv := make([][]byte, 0, len(vs))
	for _, v := range vs {
		rv = append(rv, numTerm(v))
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

// numericDocs turns one value per document into matches on field "age".
func numericDocs(t *testing.T, values []float64) []*search.DocumentMatch {
	t.Helper()
	docs := make([]*search.DocumentMatch, 0, len(values))
	for _, v := range values {
		docs = append(docs, newTestMatch(t, []string{"age"}, map[string][][]byte{"age": numTerms(v)}))
	}
	return docs
}

func consumeStats(t *testing.T, calc search.Calculator, values []float64) {
	t.Helper()
	for _, d := range numericDocs(t, values) {
		calc.Consume(d)
	}
	calc.Finish()
}

func TestStatsCalculator(t *testing.T) {
	tests := []struct {
		name         string
		values       []float64
		count        uint64
		min          float64
		max          float64
		avg          float64
		sum          float64
		sumOfSquares float64
		variance     float64
		stdDeviation float64
	}{
		{
			name: "empty",
			// elasticsearch returns null for min/max/avg here, zinc returns 0
		},
		{
			name: "single value", values: []float64{5},
			count: 1, min: 5, max: 5, avg: 5, sum: 5, sumOfSquares: 25,
		},
		{
			name: "multiple values", values: []float64{1, 2, 3, 4},
			count: 4, min: 1, max: 4, avg: 2.5, sum: 10, sumOfSquares: 30,
			variance: 1.25, stdDeviation: 1.1180339887498949,
		},
		{
			name: "negative values", values: []float64{-10, -2, 6},
			count: 3, min: -10, max: 6, avg: -2, sum: -6, sumOfSquares: 140,
			variance: 42.666666666666664, stdDeviation: 6.531972647421808,
		},
		{
			name: "constant series has no variance", values: []float64{7, 7, 7},
			count: 3, min: 7, max: 7, avg: 7, sum: 21, sumOfSquares: 147,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := NewStatsAggregation(search.Field("age")).Calculator().(*StatsCalculator)
			consumeStats(t, calc, tt.values)

			assert.Equal(t, tt.count, calc.Count())
			assert.Equal(t, tt.min, calc.Min())
			assert.Equal(t, tt.max, calc.Max())
			assert.Equal(t, tt.avg, calc.Avg())
			assert.Equal(t, tt.sum, calc.Sum())
			assert.Equal(t, tt.sumOfSquares, calc.SumOfSquares())
			assert.InDelta(t, tt.variance, calc.Variance(), 1e-9)
			assert.InDelta(t, tt.stdDeviation, calc.StdDeviation(), 1e-9)
			assert.False(t, calc.Extended())
		})
	}
}

func TestStatsAggregationFields(t *testing.T) {
	assert.Equal(t, []string{"age"}, NewStatsAggregation(search.Field("age")).Fields())
	assert.Equal(t, []string{"age"}, NewExtendedStatsAggregation(search.Field("age"), 3).Fields())
}

func TestExtendedStatsCalculator(t *testing.T) {
	calc := NewExtendedStatsAggregation(search.Field("age"), 3).Calculator().(*StatsCalculator)
	consumeStats(t, calc, []float64{1, 2, 3, 4})

	assert.True(t, calc.Extended())
	assert.Equal(t, float64(3), calc.Sigma())
	assert.Equal(t, float64(30), calc.SumOfSquares())
	assert.InDelta(t, 1.25, calc.Variance(), 1e-9)
}

func TestStatsCalculatorMerge(t *testing.T) {
	tests := []struct {
		name  string
		left  []float64
		right []float64
	}{
		{name: "both populated", left: []float64{1, 2}, right: []float64{3, 4}},
		{name: "empty left", left: nil, right: []float64{1, 2, 3, 4}},
		{name: "empty right", left: []float64{1, 2, 3, 4}, right: nil},
		{name: "both empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := NewStatsAggregation(search.Field("age"))
			left := agg.Calculator().(*StatsCalculator)
			right := agg.Calculator().(*StatsCalculator)
			consumeStats(t, left, tt.left)
			consumeStats(t, right, tt.right)
			left.Merge(right)

			want := agg.Calculator().(*StatsCalculator)
			consumeStats(t, want, append(append([]float64{}, tt.left...), tt.right...))

			assert.Equal(t, want.Count(), left.Count())
			assert.Equal(t, want.Min(), left.Min())
			assert.Equal(t, want.Max(), left.Max())
			assert.Equal(t, want.Sum(), left.Sum())
			assert.Equal(t, want.Avg(), left.Avg())
			assert.Equal(t, want.SumOfSquares(), left.SumOfSquares())
			assert.InDelta(t, want.Variance(), left.Variance(), 1e-9)
		})
	}
}

func TestStatsCalculatorMergeIgnoresOtherTypes(t *testing.T) {
	calc := NewStatsAggregation(search.Field("age")).Calculator().(*StatsCalculator)
	consumeStats(t, calc, []float64{1, 2})
	calc.Merge(NewMissingAggregation(search.Field("age")).Calculator())
	assert.Equal(t, uint64(2), calc.Count())
}

func TestValueCountCalculator(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		srcType int
		docs    []map[string][][]byte
		want    float64
	}{
		{
			name: "numeric single value per doc", field: "age", srcType: NumericValuesSource,
			docs: []map[string][][]byte{
				{"age": numTerms(1)},
				{"age": numTerms(2)},
			},
			want: 2,
		},
		{
			name: "numeric multi value counts every value", field: "age", srcType: NumericValuesSource,
			docs: []map[string][][]byte{
				{"age": numTerms(1, 2, 3)},
				{"age": numTerms(4)},
			},
			want: 4,
		},
		{
			name: "missing field is not counted", field: "age", srcType: NumericValuesSource,
			docs: []map[string][][]byte{
				{"age": numTerms(1)},
				{"name": {[]byte("bob")}},
			},
			want: 1,
		},
		{
			name: "text values", field: "name", srcType: TextValuesSource,
			docs: []map[string][][]byte{
				{"name": {[]byte("bob"), []byte("alice")}},
				{"name": {[]byte("carol")}},
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := NewValueCountAggregation(search.Field(tt.field), tt.srcType)
			assert.Equal(t, []string{tt.field}, agg.Fields())

			calc := agg.Calculator().(*ValueCountCalculator)
			for _, docVals := range tt.docs {
				calc.Consume(newTestMatch(t, []string{tt.field, "name"}, docVals))
			}
			calc.Finish()
			assert.Equal(t, tt.want, calc.Value())

			// a metric calculator so the generic response path renders it
			var _ search.MetricCalculator = calc
		})
	}
}

func TestValueCountCalculatorMerge(t *testing.T) {
	agg := NewValueCountAggregation(search.Field("age"), NumericValuesSource)
	left := agg.Calculator().(*ValueCountCalculator)
	right := agg.Calculator().(*ValueCountCalculator)
	for _, d := range numericDocs(t, []float64{1, 2}) {
		left.Consume(d)
	}
	for _, d := range numericDocs(t, []float64{3, 4, 5}) {
		right.Consume(d)
	}
	left.Merge(right)
	assert.Equal(t, float64(5), left.Value())
}

func TestPercentilesCalculator(t *testing.T) {
	agg := NewPercentilesAggregation(search.Field("age"), nil)
	assert.Equal(t, []string{"age"}, agg.Fields())

	calc := agg.Calculator().(*PercentilesCalculator)
	assert.Equal(t, DefaultPercents, calc.Percents())

	values := make([]float64, 0, 100)
	for i := 1; i <= 100; i++ {
		values = append(values, float64(i))
	}
	for _, d := range numericDocs(t, values) {
		calc.Consume(d)
	}
	calc.Finish()

	got := calc.Values()
	assert.Len(t, got, len(DefaultPercents))
	for _, key := range []string{"1.0", "5.0", "25.0", "50.0", "75.0", "95.0", "99.0"} {
		assert.Contains(t, got, key)
	}
	assert.InDelta(t, 50, got["50.0"], 1.5)
	assert.InDelta(t, 99, got["99.0"], 1.5)
}

func TestPercentilesCalculatorCustomPercents(t *testing.T) {
	calc := NewPercentilesAggregation(search.Field("age"), []float64{50, 99.9}).Calculator().(*PercentilesCalculator)
	for _, d := range numericDocs(t, []float64{10, 20, 30}) {
		calc.Consume(d)
	}
	calc.Finish()

	got := calc.Values()
	assert.Len(t, got, 2)
	assert.Contains(t, got, "50.0")
	assert.Contains(t, got, "99.9")
	assert.InDelta(t, 20, got["50.0"], 1e-9)
}

func TestPercentilesCalculatorEmptyIsZero(t *testing.T) {
	calc := NewPercentilesAggregation(search.Field("age"), []float64{50}).Calculator().(*PercentilesCalculator)
	calc.Finish()
	assert.Equal(t, map[string]float64{"50.0": 0}, calc.Values())
}

// TestPercentileKeys pins the elasticsearch key format: the requested percent
// must be readable back from the key. Rounding to one decimal reported 99.99
// under "100.0" and let 99.95 and 99.99 collide into a single entry.
func TestPercentileKeys(t *testing.T) {
	percents := []float64{1, 50, 99.9, 99.95, 99.99}
	calc := NewPercentilesAggregation(search.Field("age"), percents).Calculator().(*PercentilesCalculator)
	for _, d := range numericDocs(t, []float64{10, 20, 30}) {
		calc.Consume(d)
	}
	calc.Finish()

	got := calc.Values()
	require.Len(t, got, len(percents))
	for _, key := range []string{"1.0", "50.0", "99.9", "99.95", "99.99"} {
		assert.Contains(t, got, key)
	}
}

func TestPercentilesCalculatorMerge(t *testing.T) {
	agg := NewPercentilesAggregation(search.Field("age"), []float64{50})
	left := agg.Calculator().(*PercentilesCalculator)
	right := agg.Calculator().(*PercentilesCalculator)
	for _, d := range numericDocs(t, []float64{10, 20, 30}) {
		left.Consume(d)
	}
	for _, d := range numericDocs(t, []float64{40, 50}) {
		right.Consume(d)
	}
	left.Merge(right)
	left.Finish()

	assert.InDelta(t, 30, left.Values()["50.0"], 1e-9)
}
