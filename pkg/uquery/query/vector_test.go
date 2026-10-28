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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vcaesar/riot"

	"github.com/vcaesar/zedsearch/pkg/meta"
)

func vectorMappings() *meta.Mappings {
	m := meta.NewMappings()
	p := meta.NewProperty("vector")
	p.Dims = 3
	m.SetProperty("embedding", p)
	m.SetProperty("any", meta.NewProperty("vector"))
	m.SetProperty("name", meta.NewProperty("text"))
	hnsw := meta.NewProperty("vector")
	hnsw.Dims = 3
	hnsw.IndexOptions = &meta.VectorIndexOptions{M: 8, EfConstruction: 64}
	m.SetProperty("hnsw", hnsw)
	return m
}

func TestKnnQuery(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantField  string
		wantK      int
		wantMetric riot.Metric
		wantEf     int // 0 means exact search
		wantErr    bool
	}{
		{name: "es form defaults", query: `{"field": "embedding", "query_vector": [1, 2, 3]}`, wantField: "embedding", wantK: knnDefaultK, wantMetric: riot.Cosine},
		{name: "es form k and similarity", query: `{"field": "embedding", "query_vector": [1, 2, 3], "k": 5, "similarity": "l2_norm"}`, wantField: "embedding", wantK: 5, wantMetric: riot.L2},
		{name: "dot product", query: `{"field": "embedding", "query_vector": [1, 2, 3], "k": 2, "similarity": "dot_product"}`, wantField: "embedding", wantK: 2, wantMetric: riot.DotProduct},
		{name: "ignored options", query: `{"field": "embedding", "query_vector": [1, 2, 3], "_name": "x"}`, wantField: "embedding", wantK: knnDefaultK, wantMetric: riot.Cosine},
		{name: "num_candidates enables ann", query: `{"field": "embedding", "query_vector": [1, 2, 3], "k": 5, "num_candidates": 100}`, wantField: "embedding", wantK: 5, wantMetric: riot.Cosine, wantEf: 100},
		{name: "num_candidates equal k", query: `{"field": "embedding", "query_vector": [1, 2, 3], "k": 5, "num_candidates": 5}`, wantField: "embedding", wantK: 5, wantMetric: riot.Cosine, wantEf: 5},
		{name: "opensearch ef_search", query: `{"embedding": {"vector": [1, 2, 3], "k": 2, "method_parameters": {"ef_search": 64}}}`, wantField: "embedding", wantK: 2, wantMetric: riot.Cosine, wantEf: 64},
		{name: "num_candidates less than k", query: `{"field": "embedding", "query_vector": [1, 2, 3], "k": 5, "num_candidates": 4}`, wantErr: true},
		{name: "num_candidates default k", query: `{"field": "embedding", "query_vector": [1, 2, 3], "num_candidates": 9}`, wantErr: true},
		{name: "num_candidates too large", query: `{"field": "embedding", "query_vector": [1, 2, 3], "num_candidates": 10001}`, wantErr: true},
		{name: "num_candidates zero", query: `{"field": "embedding", "query_vector": [1, 2, 3], "num_candidates": 0}`, wantErr: true},
		{name: "num_candidates not numeric", query: `{"field": "embedding", "query_vector": [1, 2, 3], "num_candidates": "x"}`, wantErr: true},
		{name: "method_parameters not object", query: `{"embedding": {"vector": [1, 2, 3], "method_parameters": 1}}`, wantErr: true},
		{name: "method_parameters unknown", query: `{"embedding": {"vector": [1, 2, 3], "method_parameters": {"nprobes": 2}}}`, wantErr: true},
		{name: "bad ef_search", query: `{"embedding": {"vector": [1, 2, 3], "method_parameters": {"ef_search": -1}}}`, wantErr: true},
		{name: "opensearch form", query: `{"embedding": {"vector": [1, 2, 3], "k": 4, "space_type": "l2"}}`, wantField: "embedding", wantK: 4, wantMetric: riot.L2},
		{name: "any dims", query: `{"field": "any", "query_vector": [1, 2]}`, wantField: "any", wantK: knnDefaultK, wantMetric: riot.Cosine},
		{name: "dims mismatch", query: `{"field": "embedding", "query_vector": [1, 2]}`, wantErr: true},
		{name: "missing field", query: `{"query_vector": [1, 2, 3]}`, wantErr: true},
		{name: "missing vector", query: `{"field": "embedding"}`, wantErr: true},
		{name: "not vector field", query: `{"field": "name", "query_vector": [1, 2, 3]}`, wantErr: true},
		{name: "unknown field", query: `{"field": "missing", "query_vector": [1, 2, 3]}`, wantErr: true},
		{name: "bad k", query: `{"field": "embedding", "query_vector": [1, 2, 3], "k": 0}`, wantErr: true},
		{name: "bad similarity", query: `{"field": "embedding", "query_vector": [1, 2, 3], "similarity": "hamming"}`, wantErr: true},
		{name: "bad boost", query: `{"field": "embedding", "query_vector": [1, 2, 3], "boost": "x"}`, wantErr: true},
		{name: "bad vector", query: `{"field": "embedding", "query_vector": "1,2,3"}`, wantErr: true},
		{name: "zero vector cosine", query: `{"field": "embedding", "query_vector": [0, 0, 0]}`, wantErr: true},
		{name: "unknown option", query: `{"field": "embedding", "query_vector": [1, 2, 3], "bogus": 1}`, wantErr: true},
		{name: "opensearch with field", query: `{"embedding": {"vector": [1, 2, 3], "field": "x"}}`, wantErr: true},
		{name: "opensearch multiple", query: `{"embedding": {"vector": [1, 2, 3]}, "any": {"vector": [1]}}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := KnnQuery(geoQuery(t, tt.query), vectorMappings(), nil)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			q, ok := got.(*riot.KNNQuery)
			assert.True(t, ok)
			assert.Equal(t, tt.wantField, q.Field())
			assert.Equal(t, tt.wantK, q.K())
			assert.Equal(t, tt.wantMetric, q.Metric())
			if tt.wantEf == 0 {
				assert.Nil(t, q.ANN())
			} else if assert.NotNil(t, q.ANN()) {
				assert.Equal(t, tt.wantEf, q.ANN().EfSearch)
			}
		})
	}

	t.Run("mapping index_options enable ann", func(t *testing.T) {
		got, err := KnnQuery(geoQuery(t, `{"field": "hnsw", "query_vector": [1, 2, 3], "k": 3}`), vectorMappings(), nil)
		assert.NoError(t, err)
		assert.Equal(t, &riot.ANNParams{M: 8, EfConstruction: 64}, got.(*riot.KNNQuery).ANN())

		got, err = KnnQuery(geoQuery(t, `{"field": "hnsw", "query_vector": [1, 2, 3], "k": 3, "num_candidates": 40}`), vectorMappings(), nil)
		assert.NoError(t, err)
		assert.Equal(t, &riot.ANNParams{M: 8, EfConstruction: 64, EfSearch: 40}, got.(*riot.KNNQuery).ANN())
	})

	t.Run("boost", func(t *testing.T) {
		got, err := KnnQuery(geoQuery(t, `{"field": "embedding", "query_vector": [1, 2, 3], "boost": 2}`), vectorMappings(), nil)
		assert.NoError(t, err)
		assert.Equal(t, 2.0, got.(*riot.KNNQuery).Boost())
	})

	t.Run("via Query dispatcher", func(t *testing.T) {
		got, err := Query(geoQuery(t, `{"knn": {"field": "embedding", "query_vector": [1, 2, 3]}}`), vectorMappings(), nil)
		assert.NoError(t, err)
		_, ok := got.(*riot.KNNQuery)
		assert.True(t, ok)

		got, err = Query(geoQuery(t, `{"bool": {"must": {"knn": {"field": "embedding", "query_vector": [1, 2, 3]}}, "filter": {"match_all": {}}}}`), vectorMappings(), nil)
		assert.NoError(t, err)
		_, ok = got.(*riot.BooleanQuery)
		assert.True(t, ok)
	})
}

func TestKnnQuery_Extensions(t *testing.T) {
	m := vectorMappings()
	l2 := meta.NewProperty("vector")
	l2.Similarity = riot.L2
	m.SetProperty("l2", l2)

	t.Run("mapping similarity is the default metric", func(t *testing.T) {
		got, err := KnnQuery(geoQuery(t, `{"field": "l2", "query_vector": [1, 2]}`), m, nil)
		assert.NoError(t, err)
		assert.Equal(t, riot.L2, got.(*riot.KNNQuery).Metric())

		got, err = KnnQuery(geoQuery(t, `{"field": "l2", "query_vector": [1, 2], "similarity": "cosine"}`), m, nil)
		assert.NoError(t, err)
		assert.Equal(t, riot.Cosine, got.(*riot.KNNQuery).Metric())
	})

	t.Run("filter and minimum similarity wrap the query", func(t *testing.T) {
		got, err := KnnQuery(geoQuery(t, `{"field": "embedding", "query_vector": [1, 2, 3], "filter": {"term": {"name": "x"}}, "similarity": 0.5}`), m, nil)
		assert.NoError(t, err)
		q, ok := got.(*knnQuery)
		assert.True(t, ok)
		assert.NotNil(t, q.filter)
		assert.Equal(t, 0.5, *q.similarity)

		got, err = KnnQuery(geoQuery(t, `{"embedding": {"vector": [1, 2, 3], "filter": [{"term": {"name": "x"}}, {"match_all": {}}]}}`), m, nil)
		assert.NoError(t, err)
		_, ok = got.(*knnQuery).filter.(*riot.BooleanQuery)
		assert.True(t, ok)
	})

	for name, query := range map[string]string{
		"bad filter":         `{"field": "embedding", "query_vector": [1, 2, 3], "filter": {"nope": {}}}`,
		"bad filter element": `{"field": "embedding", "query_vector": [1, 2, 3], "filter": [1]}`,
		"bad similarity":     `{"field": "embedding", "query_vector": [1, 2, 3], "similarity": true}`,
		"k too large":        `{"field": "embedding", "query_vector": [1, 2, 3], "k": 10001}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := KnnQuery(geoQuery(t, query), m, nil)
			assert.Error(t, err)
		})
	}
}
