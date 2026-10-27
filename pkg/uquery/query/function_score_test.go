/* Copyright 2026 AtomAI
*
* Licensed under Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with License.
* You may obtain copy of License at
*
* http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under License is distributed on "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See License for specific language governing permissions and
* limitations under License.
 */

package query

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vcaesar/riot"
)

// functionScoreDoc is document of in-memory index used by function score tests.
type functionScoreDoc struct {
	id   string
	name string
	tag  string
	age  float64
}

var functionScoreDocs = []functionScoreDoc{
	{id: "1", name: "red apple", tag: "fruit", age: 1},
	{id: "2", name: "green apple", tag: "fruit", age: 10},
	{id: "3", name: "blue sky", tag: "sky", age: 100},
}

// newFunctionScoreIndex builds in-memory index with documents above.
func newFunctionScoreIndex(t *testing.T) *riot.Writer {
	t.Helper()

	writer, err := riot.OpenWriter(riot.InMemoryOnlyConfig())
	require.NoError(t, err)

	batch := riot.NewBatch()
	for _, d := range functionScoreDocs {
		doc := riot.NewDocument(d.id).
			AddField(riot.NewTextField("name", d.name)).
			AddField(riot.NewKeywordField("tag", d.tag).Sortable().Aggregatable()).
			AddField(riot.NewNumericField("age", d.age).Sortable().Aggregatable())
		batch.Update(riot.Identifier(d.id), doc)
	}
	require.NoError(t, writer.Batch(batch))

	return writer
}

// searchFunctionScore runs function_score query and returns score per document id.
func searchFunctionScore(t *testing.T, query map[string]interface{}) map[string]float64 {
	t.Helper()

	writer := newFunctionScoreIndex(t)
	defer func() { assert.NoError(t, writer.Close()) }()

	reader, err := writer.Reader()
	require.NoError(t, err)
	defer func() { assert.NoError(t, reader.Close()) }()

	q, err := FunctionScoreQuery(query, testMappings(), nil)
	require.NoError(t, err)

	dmi, err := reader.Search(context.Background(), riot.NewTopNSearch(10, q))
	require.NoError(t, err)

	scores := make(map[string]float64)
	match, err := dmi.Next()
	for err == nil && match != nil {
		var id string
		require.NoError(t, match.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" {
				id = string(value)
				return false
			}
			return true
		}))
		scores[id] = match.Score
		match, err = dmi.Next()
	}
	require.NoError(t, err)

	return scores
}

func matchAll() map[string]interface{} {
	return map[string]interface{}{"match_all": map[string]interface{}{}}
}

func TestFunctionScoreQuery_Weight(t *testing.T) {
	t.Run("weight replaces score", func(t *testing.T) {
		scores := searchFunctionScore(t, map[string]interface{}{
			"query":      matchAll(),
			"weight":     3.0,
			"boost_mode": "replace",
		})
		require.Len(t, scores, 3)
		for _, score := range scores {
			assert.Equal(t, 3.0, score)
		}
	})

	t.Run("weight multiplies query score", func(t *testing.T) {
		plain := searchFunctionScore(t, map[string]interface{}{
			"query":      matchAll(),
			"weight":     1.0,
			"boost_mode": "multiply",
		})
		doubled := searchFunctionScore(t, map[string]interface{}{
			"query":      matchAll(),
			"weight":     2.0,
			"boost_mode": "multiply",
		})
		for id, score := range plain {
			assert.InDelta(t, score*2, doubled[id], 1e-9)
		}
	})

	t.Run("filtered weight", func(t *testing.T) {
		scores := searchFunctionScore(t, map[string]interface{}{
			"query": matchAll(),
			"functions": []interface{}{
				map[string]interface{}{"filter": termOn("tag", "sky"), "weight": 5.0},
			},
			"boost_mode": "replace",
		})
		require.Len(t, scores, 3)
		assert.Equal(t, 5.0, scores["3"])
		// documents no function matched score with factor 1, as elasticsearch does
		assert.Equal(t, 1.0, scores["1"])
		assert.Equal(t, 1.0, scores["2"])
	})
}

func TestFunctionScoreQuery_FieldValueFactor(t *testing.T) {
	tests := []struct {
		name     string
		modifier string
		factor   float64
		want     map[string]float64
	}{
		{
			name:     "none",
			modifier: "none",
			factor:   2,
			want:     map[string]float64{"1": 2, "2": 20, "3": 200},
		},
		{
			name:     "log1p",
			modifier: "log1p",
			factor:   1,
			want:     map[string]float64{"1": math.Log10(2), "2": math.Log10(11), "3": math.Log10(101)},
		},
		{
			name:     "ln1p",
			modifier: "ln1p",
			factor:   1,
			want:     map[string]float64{"1": math.Log(2), "2": math.Log(11), "3": math.Log(101)},
		},
		{
			name:     "sqrt",
			modifier: "sqrt",
			factor:   1,
			want:     map[string]float64{"1": 1, "2": math.Sqrt(10), "3": 10},
		},
		{
			name:     "square",
			modifier: "square",
			factor:   1,
			want:     map[string]float64{"1": 1, "2": 100, "3": 10000},
		},
		{
			name:     "reciprocal",
			modifier: "reciprocal",
			factor:   1,
			want:     map[string]float64{"1": 1, "2": 0.1, "3": 0.01},
		},
		{
			name:     "log2p",
			modifier: "log2p",
			factor:   1,
			want:     map[string]float64{"1": math.Log10(3), "2": math.Log10(12), "3": math.Log10(102)},
		},
		{
			name:     "ln",
			modifier: "ln",
			factor:   1,
			want:     map[string]float64{"1": 0, "2": math.Log(10), "3": math.Log(100)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scores := searchFunctionScore(t, map[string]interface{}{
				"query": matchAll(),
				"field_value_factor": map[string]interface{}{
					"field":    "age",
					"factor":   tt.factor,
					"modifier": tt.modifier,
				},
				"boost_mode": "replace",
			})
			require.Len(t, scores, len(tt.want))
			for id, want := range tt.want {
				assert.InDelta(t, want, scores[id], 1e-9, "doc %s", id)
			}
		})
	}

	t.Run("missing value", func(t *testing.T) {
		scores := searchFunctionScore(t, map[string]interface{}{
			"query": matchAll(),
			"field_value_factor": map[string]interface{}{
				"field":   "unknown",
				"missing": 7.0,
			},
			"boost_mode": "replace",
		})
		for _, score := range scores {
			assert.Equal(t, 7.0, score)
		}
	})
}

func TestFunctionScoreQuery_Decay(t *testing.T) {
	tests := []struct {
		name string
		fn   string
		want map[string]float64
	}{
		{
			name: "gauss",
			fn:   "gauss",
			want: map[string]float64{"1": 1, "2": 0.5},
		},
		{
			name: "exp",
			fn:   "exp",
			want: map[string]float64{"1": 1, "2": 0.5},
		},
		{
			name: "linear",
			fn:   "linear",
			want: map[string]float64{"1": 1, "2": 0.5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scores := searchFunctionScore(t, map[string]interface{}{
				"query": matchAll(),
				tt.fn: map[string]interface{}{
					"age": map[string]interface{}{"origin": 1.0, "scale": 9.0, "decay": 0.5},
				},
				"boost_mode": "replace",
			})
			for id, want := range tt.want {
				assert.InDelta(t, want, scores[id], 1e-9, "doc %s", id)
			}
			// farthest document decays most
			assert.Less(t, scores["3"], scores["2"])
		})
	}

	t.Run("offset keeps full score", func(t *testing.T) {
		scores := searchFunctionScore(t, map[string]interface{}{
			"query": matchAll(),
			"gauss": map[string]interface{}{
				"age": map[string]interface{}{"origin": 1.0, "scale": 9.0, "offset": 9.0, "decay": 0.5},
			},
			"boost_mode": "replace",
		})
		assert.InDelta(t, 1.0, scores["1"], 1e-9)
		assert.InDelta(t, 1.0, scores["2"], 1e-9)
	})
}

func TestFunctionScoreQuery_RandomScore(t *testing.T) {
	query := map[string]interface{}{
		"query":        matchAll(),
		"random_score": map[string]interface{}{"seed": "42", "field": "tag"},
		"boost_mode":   "replace",
	}

	first := searchFunctionScore(t, query)
	second := searchFunctionScore(t, query)
	assert.Equal(t, first, second, "same seed must give same scores")
	for id, score := range first {
		assert.GreaterOrEqual(t, score, 0.0, "doc %s", id)
		assert.Less(t, score, 1.0, "doc %s", id)
	}
	// documents sharing field value share score
	assert.Equal(t, first["1"], first["2"])

	other := searchFunctionScore(t, map[string]interface{}{
		"query":        matchAll(),
		"random_score": map[string]interface{}{"seed": "43", "field": "tag"},
		"boost_mode":   "replace",
	})
	assert.NotEqual(t, first, other)
}

func TestFunctionScoreQuery_Modes(t *testing.T) {
	functions := []interface{}{
		map[string]interface{}{"weight": 2.0},
		map[string]interface{}{"weight": 3.0},
	}

	tests := []struct {
		scoreMode string
		want      float64
	}{
		{scoreMode: "multiply", want: 6},
		{scoreMode: "sum", want: 5},
		// weighted average: (2 + 3) / (2 + 3), not the plain mean 2.5
		{scoreMode: "avg", want: 1},
		{scoreMode: "first", want: 2},
		{scoreMode: "max", want: 3},
		{scoreMode: "min", want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.scoreMode, func(t *testing.T) {
			scores := searchFunctionScore(t, map[string]interface{}{
				"query":      matchAll(),
				"functions":  functions,
				"score_mode": tt.scoreMode,
				"boost_mode": "replace",
			})
			require.Len(t, scores, 3)
			for _, score := range scores {
				assert.InDelta(t, tt.want, score, 1e-9)
			}
		})
	}

	t.Run("boost modes", func(t *testing.T) {
		for _, mode := range []string{"multiply", "replace", "sum", "avg", "max", "min"} {
			scores := searchFunctionScore(t, map[string]interface{}{
				"query":      matchAll(),
				"weight":     4.0,
				"boost_mode": mode,
			})
			require.Len(t, scores, 3)
			for _, score := range scores {
				switch mode {
				case "replace":
					assert.Equal(t, 4.0, score)
				case "min":
					assert.Less(t, score, 4.0)
				default:
					assert.Greater(t, score, 0.0)
				}
			}
		}
	})
}

// TestFunctionScoreQuery_ScoreModeWeighting pins the elasticsearch semantics
// of score_mode avg / sum: avg divides by the sum of the weights, and a
// document no function matched is scored with factor 1 instead of keeping its
// query score.
func TestFunctionScoreQuery_ScoreModeWeighting(t *testing.T) {
	// age is 1, 10, 100; the weights turn it into 2*age and 4*age.
	functions := []interface{}{
		map[string]interface{}{"field_value_factor": map[string]interface{}{"field": "age"}, "weight": 2.0},
		map[string]interface{}{"field_value_factor": map[string]interface{}{"field": "age"}, "weight": 4.0},
	}

	tests := []struct {
		name      string
		scoreMode string
		want      map[string]float64
	}{
		{
			name:      "avg divides by the sum of the weights",
			scoreMode: "avg",
			// (age*2 + age*4) / (2 + 4) == age
			want: map[string]float64{"1": 1, "2": 10, "3": 100},
		},
		{
			name:      "sum keeps the weighted total",
			scoreMode: "sum",
			want:      map[string]float64{"1": 6, "2": 60, "3": 600},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scores := searchFunctionScore(t, map[string]interface{}{
				"query":      matchAll(),
				"functions":  functions,
				"score_mode": tt.scoreMode,
				"boost_mode": "replace",
			})
			require.Len(t, scores, 3)
			for id, want := range tt.want {
				assert.InDelta(t, want, scores[id], 1e-9, "doc %s", id)
			}
		})
	}

	t.Run("no matching function scores with factor 1", func(t *testing.T) {
		for _, mode := range []string{"multiply", "sum", "avg", "first", "max", "min"} {
			scores := searchFunctionScore(t, map[string]interface{}{
				"query": matchAll(),
				"functions": []interface{}{
					map[string]interface{}{"filter": termOn("tag", "nothing"), "weight": 5.0},
				},
				"score_mode": mode,
				"boost_mode": "replace",
			})
			require.Len(t, scores, 3)
			for id, score := range scores {
				assert.Equal(t, 1.0, score, "mode %s doc %s", mode, id)
			}
		}
	})
}

func TestFunctionScoreQuery_MaxBoostAndMinScore(t *testing.T) {
	t.Run("max_boost clamps function score", func(t *testing.T) {
		scores := searchFunctionScore(t, map[string]interface{}{
			"query":      matchAll(),
			"weight":     10.0,
			"max_boost":  2.0,
			"boost_mode": "replace",
		})
		require.Len(t, scores, 3)
		for _, score := range scores {
			assert.Equal(t, 2.0, score)
		}
	})

	t.Run("min_score drops documents", func(t *testing.T) {
		scores := searchFunctionScore(t, map[string]interface{}{
			"query": matchAll(),
			"field_value_factor": map[string]interface{}{
				"field": "age",
			},
			"boost_mode": "replace",
			"min_score":  10.0,
		})
		assert.Len(t, scores, 2)
		assert.NotContains(t, scores, "1")
		assert.Contains(t, scores, "2")
		assert.Contains(t, scores, "3")
	})

	t.Run("boost multiplies final score", func(t *testing.T) {
		scores := searchFunctionScore(t, map[string]interface{}{
			"query":      matchAll(),
			"weight":     2.0,
			"boost":      3.0,
			"boost_mode": "replace",
		})
		for _, score := range scores {
			assert.Equal(t, 6.0, score)
		}
	})
}

func TestFunctionScoreQuery_Parse(t *testing.T) {
	mappings := testMappings()

	tests := []struct {
		name    string
		query   map[string]interface{}
		wantErr string
	}{
		{
			name:    "no function",
			query:   map[string]interface{}{"query": matchAll()},
			wantErr: "[function_score] requires at least one function",
		},
		{
			name:    "unknown field",
			query:   map[string]interface{}{"weight": 1.0, "nope": 1},
			wantErr: "[function_score] unknown field [nope]",
		},
		{
			name:    "unknown score_mode",
			query:   map[string]interface{}{"weight": 1.0, "score_mode": "nope"},
			wantErr: "unknown score_mode",
		},
		{
			name:    "unknown boost_mode",
			query:   map[string]interface{}{"weight": 1.0, "boost_mode": "nope"},
			wantErr: "unknown boost_mode",
		},
		{
			name:    "field_value_factor without field",
			query:   map[string]interface{}{"field_value_factor": map[string]interface{}{"factor": 2.0}},
			wantErr: "field_value_factor requires [field]",
		},
		{
			name:    "field_value_factor unknown modifier",
			query:   map[string]interface{}{"field_value_factor": map[string]interface{}{"field": "age", "modifier": "cube"}},
			wantErr: "unknown modifier [cube]",
		},
		{
			name:    "decay without scale",
			query:   map[string]interface{}{"gauss": map[string]interface{}{"age": map[string]interface{}{"origin": 1.0}}},
			wantErr: "requires [origin] and [scale]",
		},
		{
			name:    "decay out of range",
			query:   map[string]interface{}{"gauss": map[string]interface{}{"age": map[string]interface{}{"origin": 1.0, "scale": 2.0, "decay": 1.5}}},
			wantErr: "[decay] must be in (0, 1)",
		},
		{
			name:    "two functions in one entry",
			query:   map[string]interface{}{"functions": []interface{}{map[string]interface{}{"random_score": map[string]interface{}{}, "gauss": map[string]interface{}{"age": map[string]interface{}{"origin": 1.0, "scale": 1.0}}}}},
			wantErr: "single scoring function",
		},
		{
			name:    "function without body",
			query:   map[string]interface{}{"functions": []interface{}{map[string]interface{}{"filter": termOn("tag", "a")}}},
			wantErr: "requires weight or scoring function",
		},
		{
			name:    "bad functions type",
			query:   map[string]interface{}{"functions": map[string]interface{}{}},
			wantErr: "doesn't support values of type",
		},
		{
			name:    "bad inner query",
			query:   map[string]interface{}{"query": map[string]interface{}{"nope": map[string]interface{}{}}, "weight": 1.0},
			wantErr: "[function_score] failed to parse field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := FunctionScoreQuery(tt.query, mappings, nil)
			assert.Nil(t, q)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}

	t.Run("valid query", func(t *testing.T) {
		q, err := FunctionScoreQuery(map[string]interface{}{
			"query":  matchAll(),
			"weight": 2.0,
		}, mappings, nil)
		require.NoError(t, err)
		fsq, ok := q.(*functionScoreQuery)
		require.True(t, ok)
		assert.Equal(t, 1.0, fsq.Boost())
		assert.NotNil(t, fsq.Query())
		assert.NoError(t, fsq.Validate())
	})
}

// Negative or non-finite scores are rejected as elasticsearch does; NaN / Inf
// would otherwise break the json response.
func TestFunctionScoreQuery_InvalidScore(t *testing.T) {
	tests := []struct {
		name   string
		factor map[string]interface{}
	}{
		{name: "negative", factor: map[string]interface{}{"field": "age", "factor": -1.0}},
		{name: "infinite", factor: map[string]interface{}{"field": "age", "factor": 1e308, "modifier": "square"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := newFunctionScoreIndex(t)
			defer func() { assert.NoError(t, writer.Close()) }()
			reader, err := writer.Reader()
			require.NoError(t, err)
			defer func() { assert.NoError(t, reader.Close()) }()

			q, err := FunctionScoreQuery(map[string]interface{}{
				"query":              matchAll(),
				"field_value_factor": tt.factor,
				"max_boost":          math.Inf(1),
			}, testMappings(), nil)
			require.NoError(t, err)

			dmi, err := reader.Search(context.Background(), riot.NewTopNSearch(10, q))
			if err == nil {
				_, err = dmi.Next()
			}
			assert.ErrorContains(t, err, "finite non-negative")
		})
	}
}
