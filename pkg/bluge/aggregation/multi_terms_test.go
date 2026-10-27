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

// textTerms renders plain keyword doc values.
func textTerms(vs ...string) [][]byte {
	rv := make([][]byte, 0, len(vs))
	for _, v := range vs {
		rv = append(rv, []byte(v))
	}
	return rv
}

// cityAgeDocs builds one match per (city, age) pair; an empty city or a
// negative age leaves the field without a doc value.
func cityAgeDocs(t *testing.T, cities []string, ages []float64) []*search.DocumentMatch {
	t.Helper()
	docs := make([]*search.DocumentMatch, 0, len(cities))
	for i, city := range cities {
		docVals := map[string][][]byte{}
		if city != "" {
			docVals["city"] = textTerms(city)
		}
		if ages[i] >= 0 {
			docVals["age"] = numTerms(ages[i])
		}
		docs = append(docs, newTestMatch(t, []string{"city", "age"}, docVals))
	}
	return docs
}

func runMultiTerms(t *testing.T, agg *MultiTermsAggregation, docs []*search.DocumentMatch) *MultiTermsCalculator {
	t.Helper()
	calc := agg.Calculator().(*MultiTermsCalculator)
	for _, d := range docs {
		calc.Consume(d)
	}
	calc.Finish()
	return calc
}

func bucketNames(buckets []*search.Bucket) []string {
	rv := []string{}
	for _, b := range buckets {
		rv = append(rv, b.Name())
	}
	return rv
}

func newCityAgeAggregation(size int) *MultiTermsAggregation {
	return NewMultiTermsAggregation(
		[]search.FieldSource{search.Field("city"), search.Field("age")},
		[]int{TextValueSource, NumericValueSource},
		size,
	)
}

func TestMultiTermsCalculator(t *testing.T) {
	cities := []string{"sf", "sf", "la", "sf", "", "la"}
	ages := []float64{30, 30, 40, 20, 30, -1}

	tests := []struct {
		name     string
		size     int
		order    string
		asc      bool
		useOrder bool
		want     []string
	}{
		{name: "by doc count", size: 10, want: []string{"sf|30", "la|40", "sf|20"}},
		{name: "size trims", size: 1, want: []string{"sf|30"}},
		{name: "order by key asc", size: 10, order: OrderByKey, asc: true, useOrder: true,
			want: []string{"la|40", "sf|20", "sf|30"}},
		{name: "order by count asc", size: 10, order: OrderByCount, asc: true, useOrder: true,
			want: []string{"la|40", "sf|20", "sf|30"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := newCityAgeAggregation(tt.size)
			if tt.useOrder {
				agg.SetOrder(tt.order, tt.asc)
			}
			calc := runMultiTerms(t, agg, cityAgeDocs(t, cities, ages))
			assert.Equal(t, tt.want, bucketNames(calc.Buckets()))
		})
	}
}

// Documents missing one of the fields are skipped, as in elasticsearch.
func TestMultiTermsCalculatorSkipsIncompleteDocuments(t *testing.T) {
	calc := runMultiTerms(t, newCityAgeAggregation(10), cityAgeDocs(t,
		[]string{"sf", "", "la"},
		[]float64{30, 30, -1},
	))
	assert.Equal(t, []string{"sf|30"}, bucketNames(calc.Buckets()))
	assert.Equal(t, []string{"sf", "30"}, calc.Key("sf|30"))
}

func TestMultiTermsCalculatorMerge(t *testing.T) {
	agg := newCityAgeAggregation(10)
	left := runMultiTerms(t, agg, cityAgeDocs(t, []string{"sf", "la"}, []float64{30, 40}))
	right := runMultiTerms(t, agg, cityAgeDocs(t, []string{"sf", "sf"}, []float64{30, 30}))

	left.Merge(right)
	assert.Equal(t, []string{"sf|30", "la|40"}, bucketNames(left.Buckets()))
	assert.Equal(t, uint64(3), left.Buckets()[0].Count())
}

func TestMultiTermsAggregationFields(t *testing.T) {
	agg := newCityAgeAggregation(10)
	agg.AddAggregation("avg_age", aggregations.Avg(search.Field("age")))
	assert.ElementsMatch(t, []string{"city", "age", "age"}, agg.Fields())
}
