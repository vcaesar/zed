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

package cluster

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta/elastic"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
	"github.com/vcaesar/zedsearch/test/utils"
)

func TestCatIndices(t *testing.T) {
	indexName := "TestCatIndices.index_1"

	t.Run("prepare", func(t *testing.T) {
		index, err := core.NewIndex(indexName, "disk", 2)
		assert.NoError(t, err)
		assert.NoError(t, core.StoreIndex(index))
	})

	t.Run("json format", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"target": indexName})
		utils.SetGinRequestURL(c, "/es/_cat/indices", map[string]string{"format": "json"})
		CatIndices(c)
		assert.Equal(t, http.StatusOK, w.Code)

		items := make([]elastic.CatIndexResponse, 0)
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &items))
		assert.Len(t, items, 1)
		assert.Equal(t, indexName, items[0].Index)
		assert.Equal(t, indexName, items[0].UUID)
		assert.Equal(t, "green", items[0].Health)
		assert.Equal(t, "open", items[0].Status)
		assert.Equal(t, "2", items[0].Pri)
		assert.Equal(t, "0", items[0].Rep)
		assert.Equal(t, "0", items[0].DocsDeleted)
	})

	t.Run("text format with header", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"target": indexName})
		utils.SetGinRequestURL(c, "/es/_cat/indices", map[string]string{"v": ""})
		CatIndices(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))

		lines := strings.Split(strings.TrimRight(w.Body.String(), "\n"), "\n")
		assert.Len(t, lines, 2)
		assert.True(t, strings.HasPrefix(lines[0], "health status index"), lines[0])
		assert.Contains(t, lines[1], indexName)
		// columns are whitespace aligned against the header
		assert.True(t, strings.HasPrefix(lines[1], "green  open   "), lines[1])
	})

	t.Run("text format without header", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"target": indexName})
		CatIndices(c)
		assert.Equal(t, http.StatusOK, w.Code)

		lines := strings.Split(strings.TrimRight(w.Body.String(), "\n"), "\n")
		assert.Len(t, lines, 1)
		assert.NotContains(t, w.Body.String(), "docs.count")
		assert.Contains(t, lines[0], indexName)
	})

	t.Run("no index matched", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"target": "NotExist" + indexName})
		utils.SetGinRequestURL(c, "/es/_cat/indices", map[string]string{"format": "json"})
		CatIndices(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "[]", w.Body.String())
	})

	t.Run("cleanup", func(t *testing.T) {
		assert.NoError(t, core.DeleteIndex(indexName))
	})
}

func TestCatHealth(t *testing.T) {
	t.Run("json format", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestURL(c, "/es/_cat/health", map[string]string{"format": "json"})
		CatHealth(c)
		assert.Equal(t, http.StatusOK, w.Code)

		items := make([]elastic.CatHealthResponse, 0)
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &items))
		assert.Len(t, items, 1)
		assert.Equal(t, ClusterName, items[0].Cluster)
		assert.Equal(t, "green", items[0].Status)
		assert.Equal(t, "1", items[0].NodeTotal)
		assert.Equal(t, "1", items[0].NodeData)
		assert.Equal(t, "100.0%", items[0].ActiveShardsPercent)
	})

	t.Run("text format with header", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestURL(c, "/es/_cat/health", map[string]string{"v": ""})
		CatHealth(c)
		assert.Equal(t, http.StatusOK, w.Code)

		lines := strings.Split(strings.TrimRight(w.Body.String(), "\n"), "\n")
		assert.Len(t, lines, 2)
		assert.True(t, strings.HasPrefix(lines[0], "epoch      timestamp cluster"), lines[0])
		assert.Contains(t, lines[1], ClusterName)
		assert.True(t, strings.HasSuffix(lines[1], "100.0%"))
	})
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name string
		size uint64
		want string
	}{
		{name: "zero", size: 0, want: "0b"},
		{name: "bytes", size: 512, want: "512b"},
		{name: "kilobytes", size: 2048, want: "2.0kb"},
		{name: "megabytes", size: 5 * 1024 * 1024, want: "5.0mb"},
		{name: "gigabytes", size: 3 * 1024 * 1024 * 1024, want: "3.0gb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatBytes(tt.size))
		})
	}
}
