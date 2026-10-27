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
	"github.com/stretchr/testify/require"
	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot/search"
)

// storedStubReader serves hand-built doc values and stored fields, so
// [top_hits] can be tested without opening an index.
type storedStubReader struct {
	docVals map[string][][]byte
	stored  map[string]string
	err     error
}

func (r *storedStubReader) DocumentValueReader(_ []string) (segment.DocumentValueReader, error) {
	return r, nil
}

func (r *storedStubReader) VisitDocumentValues(_ uint64, visitor segment.DocumentValueVisitor) error {
	for field, vals := range r.docVals {
		for _, v := range vals {
			visitor(field, v)
		}
	}
	return nil
}

func (r *storedStubReader) VisitStoredFields(_ uint64, visitor segment.StoredFieldVisitor) error {
	if r.err != nil {
		return r.err
	}
	for field, v := range r.stored {
		visitor(field, []byte(v))
	}
	return nil
}

// topHitsDoc builds a match with an _id / _source and an "age" doc value.
func topHitsDoc(t *testing.T, id string, score, age float64) *search.DocumentMatch {
	t.Helper()
	d := &search.DocumentMatch{Number: 1, Score: score}
	d.SetReader(&storedStubReader{
		docVals: map[string][][]byte{"age": numTerms(age)},
		stored:  map[string]string{"_id": id, "_index": "idx", "_source": `{"id":"` + id + `"}`},
	})
	require.NoError(t, d.LoadDocumentValues(search.NewSearchContext(0, 0), []string{"age"}))
	return d
}

func runTopHits(t *testing.T, agg *TopHitsAggregation, docs []*search.DocumentMatch) *TopHitsCalculator {
	t.Helper()
	calc := agg.Calculator().(*TopHitsCalculator)
	for _, d := range docs {
		calc.Consume(d)
	}
	calc.Finish()
	return calc
}

func topHitIDs(hits []TopHit) []string {
	rv := []string{}
	for _, hit := range hits {
		rv = append(rv, hit.ID)
	}
	return rv
}

func TestTopHitsCalculator(t *testing.T) {
	// id, score, age
	docs := func(t *testing.T) []*search.DocumentMatch {
		t.Helper()
		return []*search.DocumentMatch{
			topHitsDoc(t, "a", 1.5, 30),
			topHitsDoc(t, "b", 3.0, 10),
			topHitsDoc(t, "c", 2.0, 20),
		}
	}

	tests := []struct {
		name      string
		size      int
		from      int
		sortField string
		sortDesc  bool
		want      []string
	}{
		{name: "by score", size: 3, want: []string{"b", "c", "a"}},
		{name: "size limits hits", size: 2, want: []string{"b", "c"}},
		{name: "from skips hits", size: 2, from: 1, want: []string{"c", "a"}},
		{name: "from past the end", size: 2, from: 9, want: []string{}},
		{name: "sort field asc", size: 3, sortField: "age", want: []string{"b", "c", "a"}},
		{name: "sort field desc", size: 3, sortField: "age", sortDesc: true, want: []string{"a", "c", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := runTopHits(t, NewTopHitsAggregation(tt.size, tt.from, tt.sortField, tt.sortDesc), docs(t))
			require.NoError(t, calc.Err())
			assert.Equal(t, tt.want, topHitIDs(calc.Hits()))
		})
	}
}

func TestTopHitsCalculatorFields(t *testing.T) {
	assert.Nil(t, NewTopHitsAggregation(3, 0, "", false).Fields())
	assert.Equal(t, []string{"age"}, NewTopHitsAggregation(3, 0, "age", false).Fields())
}

func TestTopHitsCalculatorHit(t *testing.T) {
	calc := runTopHits(t, NewTopHitsAggregation(1, 0, "", false), []*search.DocumentMatch{topHitsDoc(t, "a", 1.5, 30)})
	require.NoError(t, calc.Err())
	require.Len(t, calc.Hits(), 1)

	hit := calc.Hits()[0]
	assert.Equal(t, "a", hit.ID)
	assert.Equal(t, "idx", hit.Index)
	assert.Equal(t, 1.5, hit.Score)
	assert.JSONEq(t, `{"id":"a"}`, string(hit.Source))
	assert.Equal(t, 1.5, calc.MaxScore())
}

func TestTopHitsCalculatorMerge(t *testing.T) {
	agg := NewTopHitsAggregation(2, 0, "", false)
	left := runTopHits(t, agg, []*search.DocumentMatch{topHitsDoc(t, "a", 1, 1), topHitsDoc(t, "b", 4, 1)})
	right := runTopHits(t, agg, []*search.DocumentMatch{topHitsDoc(t, "c", 3, 1), topHitsDoc(t, "d", 2, 1)})

	left.Merge(right)
	assert.Equal(t, []string{"b", "c"}, topHitIDs(left.Hits()))
	assert.Equal(t, uint64(4), left.Total())
}

func TestTopHitsCalculatorStoredFieldsError(t *testing.T) {
	d := &search.DocumentMatch{Number: 1, Score: 1}
	d.SetReader(&storedStubReader{err: assert.AnError})

	calc := NewTopHitsAggregation(1, 0, "", false).Calculator().(*TopHitsCalculator)
	calc.Consume(d)
	calc.Finish()

	assert.ErrorIs(t, calc.Err(), assert.AnError)
	assert.Empty(t, calc.Hits())
}
