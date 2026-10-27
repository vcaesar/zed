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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch/pkg/core"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/test/utils"
)

func TestCreateLogsIndex(t *testing.T) {
	for _, api := range []struct {
		name    string
		handler gin.HandlerFunc
	}{{"native", Create}, {"es", CreateES}} {
		for _, tt := range []struct {
			name string
			body string
			code int
		}{
			{"defaults", `{"settings":{"index_type":"logs"}}`, http.StatusOK},
			{"custom", `{"settings":{"index_type":"logs"},"mappings":{"properties":{"message":{"type":"keyword"}}}}`, http.StatusOK},
			{"unknown", `{"settings":{"index_type":"unknown"}}`, http.StatusBadRequest},
			{"bad_timestamp", `{"settings":{"index_type":"logs"},"mappings":{"properties":{"@timestamp":{"type":"text"}}}}`, http.StatusBadRequest},
		} {
			t.Run(api.name+"_"+tt.name, func(t *testing.T) {
				name := "TestCreateLogsIndex." + api.name + "_" + tt.name
				c, w := utils.NewGinContext()
				utils.SetGinRequestData(c, tt.body)
				utils.SetGinRequestParams(c, map[string]string{"target": name})
				api.handler(c)
				require.Equal(t, tt.code, w.Code, w.Body.String())
				index, exists := core.GetIndex(name)
				if tt.code != http.StatusOK {
					assert.False(t, exists)
					return
				}
				require.True(t, exists)
				t.Cleanup(func() { assert.NoError(t, core.DeleteIndex(name)) })
				assert.Equal(t, meta.IndexTypeLogs, index.GetIndexType())
				prop, ok := index.GetMappings().GetProperty("level")
				assert.True(t, ok)
				assert.Equal(t, "keyword", prop.Type)
				if tt.name == "custom" {
					prop, ok = index.GetMappings().GetProperty("message")
					assert.True(t, ok)
					assert.Equal(t, "keyword", prop.Type)
				}
			})
		}
	}
}

func TestLogsIndexSettings(t *testing.T) {
	name := "TestLogsIndexSettings.index_1"
	c, w := utils.NewGinContext()
	utils.SetGinRequestParams(c, map[string]string{"target": name})
	utils.SetGinRequestData(c, `{"index_type":"logs"}`)
	SetSettings(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	t.Cleanup(func() { assert.NoError(t, core.DeleteIndex(name)) })
	index, ok := core.GetIndex(name)
	require.True(t, ok)
	assert.Equal(t, meta.IndexTypeLogs, index.GetIndexType())

	for _, tt := range []struct {
		body string
		code int
	}{
		{`{"index_type":"standard","number_of_replicas":5}`, http.StatusBadRequest},
		{`{"index_type":"unknown"}`, http.StatusBadRequest},
		{`{"index_type":"logs"}`, http.StatusOK},
	} {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"target": name})
		utils.SetGinRequestData(c, tt.body)
		SetSettings(c)
		assert.Equal(t, tt.code, w.Code, w.Body.String())
		assert.Equal(t, meta.IndexTypeLogs, index.GetIndexType())
		assert.Zero(t, index.GetSettings().NumberOfReplicas)
	}

	c, w = utils.NewGinContext()
	utils.SetGinRequestParams(c, map[string]string{"target": name})
	GetSettings(c)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"index_type":"logs"`)

	c, w = utils.NewGinContext()
	utils.SetGinRequestParams(c, map[string]string{"target": name + ".bad"})
	utils.SetGinRequestData(c, `{"index_type":"unknown"}`)
	SetSettings(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	_, exists := core.GetIndex(name + ".bad")
	assert.False(t, exists)
}
