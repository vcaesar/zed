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

package query

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vcaesar/riot"
)

func knnReader(t *testing.T, n int) *riot.Reader {
	t.Helper()
	w, err := riot.OpenWriter(riot.InMemoryOnlyConfig())
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, w.Close()) })
	for i := 0; i < n; i++ {
		f, err := riot.NewVectorField("v", []float32{float32(i), 1})
		assert.NoError(t, err)
		d := riot.NewDocument(fmt.Sprint(i)).AddField(f).AddField(riot.NewKeywordField("even", fmt.Sprint(i%2 == 0)))
		assert.NoError(t, w.Update(d.ID(), d))
	}
	r, err := w.Reader()
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, r.Close()) })
	return r
}

func knnSearch(t *testing.T, r *riot.Reader, q riot.Query, explain bool) (ids []string, scores []float64) {
	t.Helper()
	req := riot.NewTopNSearch(100, q)
	if explain {
		req.ExplainScores()
	}
	itr, err := r.Search(context.Background(), req)
	assert.NoError(t, err)
	for {
		m, err := itr.Next()
		assert.NoError(t, err)
		if m == nil {
			return ids, scores
		}
		if explain {
			assert.NotNil(t, m.Explanation)
		}
		assert.NoError(t, m.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" {
				ids = append(ids, string(value))
			}
			return true
		}))
		scores = append(scores, m.Score)
	}
}

func TestKnnSearcher(t *testing.T) {
	r := knnReader(t, 40)
	even := riot.NewTermQuery("true").SetField("even")
	newQuery := func(ann *riot.ANNParams, filter riot.Query, min *float64) *knnQuery {
		knn := riot.NewKNNQuery("v", []float32{10, 1}, 3).SetMetric(riot.L2).SetBoost(2)
		if ann != nil {
			knn.SetANN(*ann)
		}
		return &knnQuery{knn: knn, filter: filter, similarity: min}
	}

	for name, ann := range map[string]*riot.ANNParams{
		"exact":              nil,
		"ann exact fallback": {},
		"ann graph":          {M: 2, EfSearch: 3}, // 20 filtered docs > max(3, k)*2
	} {
		t.Run(name, func(t *testing.T) {
			ids, scores := knnSearch(t, r, newQuery(ann, even, nil), true)
			assert.ElementsMatch(t, []string{"10", "8", "12"}, ids)
			assert.Contains(t, scores, 0.0)
			assert.Contains(t, scores, -8.0) // boost 2 * -(2^2)
		})
	}

	t.Run("minimum similarity is the l2 distance", func(t *testing.T) {
		min := 1.5
		ids, _ := knnSearch(t, r, newQuery(nil, nil, &min), false)
		assert.ElementsMatch(t, []string{"9", "10", "11"}, ids)

		min = 0.5
		ids, _ = knnSearch(t, r, newQuery(nil, even, &min), false)
		assert.Equal(t, []string{"10"}, ids)
	})

	t.Run("filter matching nothing", func(t *testing.T) {
		ids, _ := knnSearch(t, r, newQuery(&riot.ANNParams{}, riot.NewTermQuery("none").SetField("even"), nil), false)
		assert.Empty(t, ids)
	})

	t.Run("bool filter is a post-filter", func(t *testing.T) {
		q := riot.NewBooleanQuery().AddMust(riot.NewTermQuery("true").SetField("even")).AddMust(newQuery(nil, nil, nil))
		ids, _ := knnSearch(t, r, q, false)
		// a bool filter applies after ranking: top 3 is 9, 10, 11, of which only 10 is even
		assert.Equal(t, []string{"10"}, ids)
	})
}
