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

package mappings

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

func TestRequest_VectorAndGeo(t *testing.T) {
	tests := []struct {
		name     string
		props    string
		wantType string
		wantDims int
		wantErr  bool
	}{
		{name: "vector with dims", props: `{"emb": {"type": "vector", "dims": 3}}`, wantType: "vector", wantDims: 3},
		{name: "dense_vector alias", props: `{"emb": {"type": "dense_vector", "dims": 4}}`, wantType: "vector", wantDims: 4},
		{name: "knn_vector alias", props: `{"emb": {"type": "knn_vector", "dimension": 2}}`, wantType: "vector", wantDims: 2},
		{name: "vector without dims", props: `{"emb": {"type": "vector"}}`, wantType: "vector"},
		{name: "vector sortable forced off", props: `{"emb": {"type": "vector", "sortable": true, "aggregatable": true, "highlightable": true}}`, wantType: "vector"},
		{name: "vector zero dims", props: `{"emb": {"type": "vector", "dims": 0}}`, wantErr: true},
		{name: "vector negative dims", props: `{"emb": {"type": "vector", "dims": -1}}`, wantErr: true},
		{name: "vector string dims", props: `{"emb": {"type": "vector", "dims": "x"}}`, wantErr: true},
		{name: "dims on text", props: `{"msg": {"type": "text", "dims": 3}}`, wantErr: true},
		{name: "dims on geo_point", props: `{"loc": {"type": "geo_point", "dims": 2}}`, wantErr: true},
		{name: "geo_point", props: `{"loc": {"type": "geo_point"}}`, wantType: "geo_point"},
		{name: "geo alias", props: `{"loc": {"type": "geo"}}`, wantType: "geo_point"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var props map[string]interface{}
			assert.NoError(t, json.Unmarshal([]byte(tt.props), &props))
			m, err := Request(nil, map[string]interface{}{"properties": props})
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, 1, m.Len())
			for _, p := range m.ListProperty() {
				assert.Equal(t, tt.wantType, p.Type)
				assert.Equal(t, tt.wantDims, p.Dims)
				if p.Type == "vector" {
					assert.False(t, p.Sortable)
					assert.False(t, p.Aggregatable)
					assert.False(t, p.Highlightable)
				}
			}
		})
	}
}

func TestRequest_NestedAndJoin(t *testing.T) {
	t.Run("nested with properties", func(t *testing.T) {
		m, err := Request(nil, map[string]interface{}{"properties": map[string]interface{}{
			"comments": map[string]interface{}{
				"type": "nested",
				"properties": map[string]interface{}{
					"name":  map[string]interface{}{"type": "keyword"},
					"stars": map[string]interface{}{"type": "numeric"},
				},
			},
		}})
		assert.NoError(t, err)

		nested, ok := m.GetProperty("comments")
		assert.True(t, ok)
		assert.Equal(t, "nested", nested.Type)
		assert.False(t, nested.Sortable)
		assert.Len(t, nested.Properties, 2)

		name, ok := m.GetProperty("comments.name")
		assert.True(t, ok)
		assert.Equal(t, "keyword", name.Type)
		stars, ok := m.GetProperty("comments.stars")
		assert.True(t, ok)
		assert.Equal(t, "numeric", stars.Type)

		for _, field := range []string{meta.NestedPathField, meta.NestedParentField} {
			prop, ok := m.GetProperty(field)
			assert.True(t, ok, field)
			assert.Equal(t, "keyword", prop.Type)
			assert.True(t, prop.Sortable, field)
		}
	})

	t.Run("nested without properties", func(t *testing.T) {
		m, err := Request(nil, map[string]interface{}{"properties": map[string]interface{}{
			"comments": map[string]interface{}{"type": "nested"},
		}})
		assert.NoError(t, err)
		nested, ok := m.GetProperty("comments")
		assert.True(t, ok)
		assert.Equal(t, "nested", nested.Type)
		_, ok = m.GetProperty(meta.NestedPathField)
		assert.True(t, ok)
	})

	t.Run("join", func(t *testing.T) {
		m, err := Request(nil, map[string]interface{}{"properties": map[string]interface{}{
			"my_join": map[string]interface{}{
				"type":      "join",
				"relations": map[string]interface{}{"question": []interface{}{"answer", "comment"}},
			},
		}})
		assert.NoError(t, err)

		join, ok := m.GetProperty("my_join")
		assert.True(t, ok)
		assert.Equal(t, "join", join.Type)
		assert.Equal(t, map[string][]string{"question": {"answer", "comment"}}, join.Relations)

		for _, field := range []string{meta.JoinNameField, meta.JoinParentField} {
			prop, ok := m.GetProperty(field)
			assert.True(t, ok, field)
			assert.Equal(t, "keyword", prop.Type)
		}
	})

	t.Run("join with single child", func(t *testing.T) {
		m, err := Request(nil, map[string]interface{}{"properties": map[string]interface{}{
			"my_join": map[string]interface{}{"type": "join", "relations": map[string]interface{}{"question": "answer"}},
		}})
		assert.NoError(t, err)
		join, _ := m.GetProperty("my_join")
		assert.Equal(t, map[string][]string{"question": {"answer"}}, join.Relations)
	})

	errTests := []struct {
		name    string
		props   map[string]interface{}
		wantErr string
	}{
		{
			name:    "join without relations",
			props:   map[string]interface{}{"my_join": map[string]interface{}{"type": "join"}},
			wantErr: "requires [relations] object",
		},
		{
			name:    "join with empty relations",
			props:   map[string]interface{}{"my_join": map[string]interface{}{"type": "join", "relations": map[string]interface{}{}}},
			wantErr: "requires [relations] object",
		},
		{
			name:    "join with bad child type",
			props:   map[string]interface{}{"my_join": map[string]interface{}{"type": "join", "relations": map[string]interface{}{"question": 1.0}}},
			wantErr: "should be string or array of strings",
		},
		{
			name:    "join child equal parent",
			props:   map[string]interface{}{"my_join": map[string]interface{}{"type": "join", "relations": map[string]interface{}{"question": []interface{}{"question"}}}},
			wantErr: "invalid child [question]",
		},
		{
			name:    "join duplicated child",
			props:   map[string]interface{}{"my_join": map[string]interface{}{"type": "join", "relations": map[string]interface{}{"question": []interface{}{"answer", "answer"}}}},
			wantErr: "duplicated child [answer]",
		},
	}
	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Request(nil, map[string]interface{}{"properties": tt.props})
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
