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

package index

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/metadata"
	"github.com/vcaesar/zedsearch/test/utils"
)

func policyBody(phases map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"policy": map[string]interface{}{"phases": phases}}
}

func TestILMPolicyCRUD(t *testing.T) {
	name := "TestILMPolicyCRUD.policy_1"
	t.Cleanup(func() { _ = metadata.ILM.Delete(name) })

	t.Run("put", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, policyBody(map[string]interface{}{
			"warm":   map[string]interface{}{"min_age": "7d"},
			"cold":   map[string]interface{}{"min_age": "30d"},
			"delete": map[string]interface{}{"min_age": "90d"},
		}))
		utils.SetGinRequestParams(c, map[string]string{"name": name})
		PutILMPolicy(c)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, `{"message":"ok"}`, w.Body.String())
	})

	t.Run("get", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"name": name})
		GetILMPolicy(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"min_age":"7d"`)
		assert.Contains(t, w.Body.String(), `"version":1`)
	})

	t.Run("put again bumps the version", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, policyBody(map[string]interface{}{
			"cold": map[string]interface{}{"min_age": "10d"},
		}))
		utils.SetGinRequestParams(c, map[string]string{"name": name})
		PutILMPolicy(c)
		require.Equal(t, http.StatusOK, w.Code)

		record, err := metadata.ILM.Get(name)
		require.NoError(t, err)
		assert.Equal(t, int64(2), record.Version)
		assert.Equal(t, "10d", record.Policy.Phases["cold"].MinAge)
	})

	t.Run("list", func(t *testing.T) {
		c, w := utils.NewGinContext()
		ListILMPolicy(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), name)
	})

	t.Run("get a missing policy", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"name": "TestILMPolicyCRUD.missing"})
		GetILMPolicy(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("delete", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"name": name})
		DeleteILMPolicy(c)
		assert.Equal(t, http.StatusOK, w.Code)
		_, err := metadata.ILM.Get(name)
		assert.Error(t, err)
	})
}

func TestILMPolicyValidation(t *testing.T) {
	tests := []struct {
		name string
		body interface{}
		key  string
		code int
	}{
		{
			name: "empty name",
			body: policyBody(map[string]interface{}{"warm": map[string]interface{}{"min_age": "7d"}}),
			key:  "",
			code: http.StatusBadRequest,
		},
		{
			name: "no phases",
			body: policyBody(nil),
			key:  "TestILMPolicyValidation.policy_1",
			code: http.StatusBadRequest,
		},
		{
			name: "unknown phase",
			body: policyBody(map[string]interface{}{"frozen": map[string]interface{}{"min_age": "7d"}}),
			key:  "TestILMPolicyValidation.policy_2",
			code: http.StatusBadRequest,
		},
		{
			name: "invalid min_age",
			body: policyBody(map[string]interface{}{"warm": map[string]interface{}{"min_age": "7 fortnights"}}),
			key:  "TestILMPolicyValidation.policy_3",
			code: http.StatusBadRequest,
		},
		{
			name: "bad json",
			body: `{"policy":}`,
			key:  "TestILMPolicyValidation.policy_4",
			code: http.StatusBadRequest,
		},
		{
			name: "hot phase without min_age",
			body: policyBody(map[string]interface{}{"hot": map[string]interface{}{}, "cold": map[string]interface{}{"min_age": "1d"}}),
			key:  "TestILMPolicyValidation.policy_5",
			code: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := utils.NewGinContext()
			utils.SetGinRequestData(c, tt.body)
			utils.SetGinRequestParams(c, map[string]string{"name": tt.key})
			PutILMPolicy(c)
			assert.Equal(t, tt.code, w.Code, w.Body.String())
			if tt.code == http.StatusOK {
				assert.NoError(t, metadata.ILM.Delete(tt.key))
			}
		})
	}
}

func TestExplainILM(t *testing.T) {
	name := "TestExplainILM.index_1"
	policy := "TestExplainILM.policy_1"

	index, err := core.NewIndex(name, "disk", 1)
	require.NoError(t, err)
	require.NoError(t, core.StoreIndex(index))
	t.Cleanup(func() { _ = core.DeleteIndex(name) })

	require.NoError(t, metadata.ILM.Set(policy, metadata.ILMPolicyRecord{
		Policy: metadata.ILMPolicy{Phases: map[string]metadata.ILMPhase{
			"warm": {MinAge: "7d"},
			"cold": {MinAge: "30d"},
		}},
	}))
	t.Cleanup(func() { _ = metadata.ILM.Delete(policy) })

	t.Run("unmanaged", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"target": name})
		ExplainILM(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"managed":false`)
		assert.Contains(t, w.Body.String(), `"tier":"hot"`)
	})

	t.Run("managed", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, map[string]interface{}{
			"lifecycle": map[string]interface{}{"policy": policy},
		})
		utils.SetGinRequestParams(c, map[string]string{"target": name})
		SetSettings(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		c, w = utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"target": name})
		ExplainILM(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"managed":true`)
		assert.Contains(t, w.Body.String(), `"policy":"`+policy+`"`)
		assert.Contains(t, w.Body.String(), `"age"`)
		assert.Contains(t, w.Body.String(), `"action"`)
	})

	t.Run("missing index", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"target": "TestExplainILM.index_missing"})
		ExplainILM(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
