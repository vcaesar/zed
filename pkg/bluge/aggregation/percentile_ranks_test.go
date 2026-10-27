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
	"github.com/vcaesar/riot/search"
)

func runPercentileRanks(t *testing.T, values, docValues []float64) *PercentileRanksCalculator {
	t.Helper()
	calc := NewPercentileRanksAggregation(search.Field("age"), values).Calculator().(*PercentileRanksCalculator)
	for _, d := range numericDocs(t, docValues) {
		calc.Consume(d)
	}
	calc.Finish()
	return calc
}

func TestPercentileRanksCalculator(t *testing.T) {
	tests := []struct {
		name      string
		values    []float64
		docValues []float64
		want      map[string]float64
	}{
		{
			name:   "no documents yields zero ranks",
			values: []float64{500},
			want:   map[string]float64{"500.0": 0},
		},
		{
			name:      "below every observation",
			values:    []float64{0},
			docValues: []float64{10, 20, 30, 40},
			want:      map[string]float64{"0.0": 0},
		},
		{
			name:      "above every observation",
			values:    []float64{100},
			docValues: []float64{10, 20, 30, 40},
			want:      map[string]float64{"100.0": 100},
		},
		{
			name:      "middle of a uniform series",
			values:    []float64{50},
			docValues: []float64{0, 25, 50, 75, 100},
			want:      map[string]float64{"50.0": 50},
		},
		{
			// rounding the key to one decimal reported 10.25 under "10.2"
			// and made 10.25 and 10.3 collide into a single entry.
			name:      "fractional values keep their key",
			values:    []float64{10.25, 10.3},
			docValues: []float64{0, 100},
			want:      map[string]float64{"10.25": 30.125, "10.3": 30.15},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := runPercentileRanks(t, tt.values, tt.docValues)
			require.NoError(t, calc.Err())
			assert.Equal(t, tt.want, calc.Values())
		})
	}
}

func TestPercentileRanksCalculatorMerge(t *testing.T) {
	left := runPercentileRanks(t, []float64{50}, []float64{0, 25})
	right := runPercentileRanks(t, []float64{50}, []float64{50, 75, 100})

	left.Merge(right)
	require.NoError(t, left.Err())
	assert.Equal(t, map[string]float64{"50.0": 50}, left.Values())
}

func TestPercentileRanksAggregationFields(t *testing.T) {
	assert.Equal(t, []string{"age"}, NewPercentileRanksAggregation(search.Field("age"), []float64{1}).Fields())
}
