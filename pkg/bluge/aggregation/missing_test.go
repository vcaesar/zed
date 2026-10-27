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
)

func TestMissingCalculator(t *testing.T) {
	tests := []struct {
		name   string
		field  string
		fields []string
		docs   []map[string][][]byte
		want   uint64
	}{
		{
			name: "no document misses the field", field: "age", fields: []string{"age"},
			docs: []map[string][][]byte{
				{"age": numTerms(1)},
				{"age": numTerms(2)},
			},
			want: 0,
		},
		{
			name: "numeric field missing", field: "age", fields: []string{"age", "name"},
			docs: []map[string][][]byte{
				{"age": numTerms(1)},
				{"name": {[]byte("bob")}},
				{"name": {[]byte("alice")}},
			},
			want: 2,
		},
		{
			name: "text field missing", field: "name", fields: []string{"age", "name"},
			docs: []map[string][][]byte{
				{"name": {[]byte("bob")}},
				{"age": numTerms(1)},
			},
			want: 1,
		},
		{
			name: "unmapped field counts every document", field: "nope", fields: []string{"nope", "age"},
			docs: []map[string][][]byte{
				{"age": numTerms(1)},
				{"age": numTerms(2)},
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := NewMissingAggregation(search.Field(tt.field))
			assert.Equal(t, []string{tt.field}, agg.Fields())

			calc := agg.Calculator().(*MissingCalculator)
			for _, docVals := range tt.docs {
				calc.Consume(newTestMatch(t, tt.fields, docVals))
			}
			calc.Finish()
			assert.Equal(t, tt.want, calc.DocCount())
		})
	}
}

func TestMissingCalculatorMerge(t *testing.T) {
	agg := NewMissingAggregation(search.Field("age"))
	left := agg.Calculator().(*MissingCalculator)
	right := agg.Calculator().(*MissingCalculator)

	left.Consume(newTestMatch(t, []string{"age", "name"}, map[string][][]byte{"name": {[]byte("bob")}}))
	right.Consume(newTestMatch(t, []string{"age", "name"}, map[string][][]byte{"name": {[]byte("alice")}}))
	right.Consume(newTestMatch(t, []string{"age", "name"}, map[string][][]byte{"age": numTerms(3)}))

	left.Merge(right)
	assert.Equal(t, uint64(2), left.DocCount())
}

func TestMissingCalculatorMergeIgnoresOtherTypes(t *testing.T) {
	calc := NewMissingAggregation(search.Field("age")).Calculator().(*MissingCalculator)
	calc.Consume(newTestMatch(t, []string{"age", "name"}, map[string][][]byte{"name": {[]byte("bob")}}))
	calc.Merge(NewStatsAggregation(search.Field("age")).Calculator())
	assert.Equal(t, uint64(1), calc.DocCount())
}
