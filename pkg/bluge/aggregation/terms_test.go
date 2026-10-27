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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

func TestTermsAggregationSetOrder(t *testing.T) {
	// city -> ages; "sf" has the most documents, "la" the highest average age
	cities := []string{"sf", "sf", "sf", "la", "la", "nyc"}
	ages := []float64{10, 20, 30, 90, 80, 50}

	tests := []struct {
		name     string
		order    string
		asc      bool
		useOrder bool
		want     []string
	}{
		{name: "default is count desc", want: []string{"sf", "la", "nyc"}},
		{name: "count asc", order: OrderByCount, asc: true, useOrder: true, want: []string{"nyc", "la", "sf"}},
		{name: "key asc", order: OrderByKey, asc: true, useOrder: true, want: []string{"la", "nyc", "sf"}},
		{name: "key desc", order: OrderByKey, useOrder: true, want: []string{"sf", "nyc", "la"}},
		{name: "sub aggregation desc", order: "avg_age", useOrder: true, want: []string{"la", "nyc", "sf"}},
		{name: "sub aggregation asc", order: "avg_age", asc: true, useOrder: true, want: []string{"sf", "nyc", "la"}},
		{name: "stats metric desc", order: "age_stats.max", useOrder: true, want: []string{"la", "nyc", "sf"}},
		{name: "unknown key keeps insertion order", order: "nope", useOrder: true, want: []string{"sf", "la", "nyc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := NewTermsAggregation(search.Field("city"), TextValueSource, 10)
			agg.AddAggregation("avg_age", aggregations.Avg(search.Field("age")))
			agg.AddAggregation("age_stats", NewStatsAggregation(search.Field("age")))
			if tt.useOrder {
				agg.SetOrder(tt.order, tt.asc)
			}

			calc := agg.Calculator().(*TermsCalculator)
			for _, d := range cityAgeDocs(t, cities, ages) {
				calc.Consume(d)
			}
			calc.Finish()
			assert.Equal(t, tt.want, bucketNames(calc.Buckets()))
		})
	}
}
