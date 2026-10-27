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

package index

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zincsearch/zincsearch/pkg/core"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/meta/elastic"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
	"github.com/zincsearch/zincsearch/test/utils"
)

func TestFieldCaps(t *testing.T) {
	indexName := "TestFieldCaps.index_1"

	t.Run("prepare", func(t *testing.T) {
		index, err := core.NewIndex(indexName, "disk", 2)
		assert.NoError(t, err)
		mappings := meta.NewMappings()
		mappings.SetProperty("name", meta.NewProperty("text"))
		mappings.SetProperty("age", meta.NewProperty("numeric"))
		mappings.SetProperty("tag", meta.NewProperty("keyword"))
		mappings.SetProperty("vec", meta.NewProperty("vector"))
		assert.NoError(t, index.SetMappings(mappings))
		assert.NoError(t, core.StoreIndex(index))
	})

	type args struct {
		code    int
		data    string
		query   map[string]string
		params  map[string]string
		fields  map[string]string // field name -> expected es type
		missing []string
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "all fields",
			args: args{
				code:   http.StatusOK,
				query:  map[string]string{"fields": "*"},
				params: map[string]string{"target": indexName},
				fields: map[string]string{
					"name": "text",
					"age":  "double",
					"tag":  "keyword",
					"vec":  "dense_vector",
					"_id":  "keyword",
				},
			},
		},
		{
			name: "glob field pattern",
			args: args{
				code:    http.StatusOK,
				query:   map[string]string{"fields": "na*"},
				params:  map[string]string{"target": indexName},
				fields:  map[string]string{"name": "text"},
				missing: []string{"age", "tag", "vec"},
			},
		},
		{
			name: "fields from body",
			args: args{
				code:    http.StatusOK,
				data:    `{"fields":["age"]}`,
				params:  map[string]string{"target": indexName},
				fields:  map[string]string{"age": "double"},
				missing: []string{"name"},
			},
		},
		{
			name: "index glob",
			args: args{
				code:   http.StatusOK,
				query:  map[string]string{"fields": "tag"},
				params: map[string]string{"target": "TestFieldCaps.*"},
				fields: map[string]string{"tag": "keyword"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := utils.NewGinContext()
			utils.SetGinRequestData(c, tt.args.data)
			utils.SetGinRequestParams(c, tt.args.params)
			utils.SetGinRequestURL(c, "/es/_field_caps", tt.args.query)
			FieldCaps(c)
			assert.Equal(t, tt.args.code, w.Code)

			resp := new(elastic.FieldCapsResponse)
			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), resp))
			assert.Contains(t, resp.Indices, indexName)
			for field, typ := range tt.args.fields {
				caps, ok := resp.Fields[field]
				assert.True(t, ok, "field %s missing", field)
				assert.Equal(t, elastic.FieldCap{
					Type:          typ,
					MetadataField: false,
					Searchable:    true,
					Aggregatable:  typ != "text" && typ != "dense_vector",
				}, caps[typ])
			}
			for _, field := range tt.args.missing {
				_, ok := resp.Fields[field]
				assert.False(t, ok, "field %s should not be returned", field)
			}
		})
	}

	t.Run("index not found", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, "")
		utils.SetGinRequestParams(c, map[string]string{"target": "NotExist" + indexName})
		FieldCaps(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "does not exists")
	})

	t.Run("body json error", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, `{"fields":[x]}`)
		utils.SetGinRequestParams(c, map[string]string{"target": indexName})
		FieldCaps(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid character")
	})

	t.Run("nil body", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"target": indexName})
		FieldCaps(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"name"`)
	})

	t.Run("cleanup", func(t *testing.T) {
		assert.NoError(t, core.DeleteIndex(indexName))
	})
}

func TestESFieldType(t *testing.T) {
	tests := []struct {
		name string
		typ  string
		want string
	}{
		{name: "text", typ: "text", want: "text"},
		{name: "keyword", typ: "keyword", want: "keyword"},
		{name: "date", typ: "date", want: "date"},
		{name: "numeric", typ: "numeric", want: "double"},
		{name: "bool", typ: "bool", want: "boolean"},
		{name: "boolean", typ: "boolean", want: "boolean"},
		{name: "geo_point", typ: "geo_point", want: "geo_point"},
		{name: "vector", typ: "vector", want: "dense_vector"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, esFieldType(tt.typ))
		})
	}
}
