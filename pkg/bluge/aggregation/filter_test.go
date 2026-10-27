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
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

// nestedPathField mirrors meta.NestedPathField; pkg/meta imports this package
// so the test cannot.
const nestedPathField = "_nested_path"

// cityIs matches documents whose "city" doc value is city.
func cityIs(city string) *FilterPredicate {
	return &FilterPredicate{
		Fields: []string{"city"},
		Match: func(d *search.DocumentMatch) bool {
			for _, v := range search.Field("city").Values(d) {
				if string(v) == city {
					return true
				}
			}
			return false
		},
	}
}

func filterDocs(t *testing.T, cities ...string) []*search.DocumentMatch {
	t.Helper()
	return cityAgeDocs(t, cities, make([]float64, len(cities)))
}

func TestFilterCalculator(t *testing.T) {
	tests := []struct {
		name     string
		pred     *FilterPredicate
		cities   []string
		docCount uint64
	}{
		{name: "matches a subset", pred: cityIs("sf"), cities: []string{"sf", "la", "sf"}, docCount: 2},
		{name: "matches nothing", pred: cityIs("nyc"), cities: []string{"sf", "la"}, docCount: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := NewFilterAggregation(tt.pred)
			calc := agg.Calculator().(*FilterCalculator)
			for _, d := range filterDocs(t, tt.cities...) {
				calc.Consume(d)
			}
			calc.Finish()
			assert.Equal(t, tt.docCount, calc.DocCount())
		})
	}
}

func TestFilterAggregationFields(t *testing.T) {
	agg := NewFilterAggregation(cityIs("sf"))
	agg.AddAggregation("avg_age", aggregations.Avg(search.Field("age")))
	assert.ElementsMatch(t, []string{"city", "age"}, agg.Fields())
}

func TestFiltersCalculator(t *testing.T) {
	tests := []struct {
		name     string
		otherKey string
		want     map[string]uint64
	}{
		{name: "named buckets", want: map[string]uint64{"bay": 2, "socal": 1}},
		{name: "other bucket", otherKey: "other", want: map[string]uint64{"bay": 2, "socal": 1, "other": 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := NewFiltersAggregation(tt.otherKey)
			agg.AddFilter("bay", cityIs("sf"))
			agg.AddFilter("socal", cityIs("la"))

			calc := agg.Calculator().(*FiltersCalculator)
			for _, d := range filterDocs(t, "sf", "la", "sf", "nyc") {
				calc.Consume(d)
			}
			calc.Finish()

			got := map[string]uint64{}
			for _, b := range calc.NamedBuckets() {
				got[b.Name()] = b.Count()
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

// A document may land in several filters buckets at once, as in elasticsearch.
func TestFiltersCalculatorOverlappingFilters(t *testing.T) {
	agg := NewFiltersAggregation("")
	agg.AddFilter("sf", cityIs("sf"))
	agg.AddFilter("also_sf", cityIs("sf"))

	calc := agg.Calculator().(*FiltersCalculator)
	for _, d := range filterDocs(t, "sf") {
		calc.Consume(d)
	}
	calc.Finish()

	for _, b := range calc.NamedBuckets() {
		assert.Equal(t, uint64(1), b.Count(), b.Name())
	}
}

func TestNestedAggregation(t *testing.T) {
	nestedDoc := newTestMatch(t, []string{nestedPathField},
		map[string][][]byte{nestedPathField: textTerms("comments")})
	rootDoc := newTestMatch(t, []string{nestedPathField}, map[string][][]byte{})

	tests := []struct {
		name     string
		agg      *FilterAggregation
		docCount uint64
	}{
		{name: "nested keeps nested documents", agg: NewNestedAggregation(nestedPathField, "comments"), docCount: 1},
		{name: "nested ignores other paths", agg: NewNestedAggregation(nestedPathField, "tags"), docCount: 0},
		{name: "reverse_nested keeps root documents", agg: NewReverseNestedAggregation(nestedPathField), docCount: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := tt.agg.Calculator().(*FilterCalculator)
			calc.Consume(nestedDoc)
			calc.Consume(rootDoc)
			calc.Finish()
			assert.Equal(t, tt.docCount, calc.DocCount())
		})
	}
}
