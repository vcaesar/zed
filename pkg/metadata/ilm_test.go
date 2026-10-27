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

package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestILM(t *testing.T) {
	name := "TestILM.policy_1"
	record := ILMPolicyRecord{
		Version:      1,
		ModifiedDate: 1700000000000,
		Policy: ILMPolicy{Phases: map[string]ILMPhase{
			"warm":   {MinAge: "7d"},
			"cold":   {MinAge: "30d"},
			"delete": {MinAge: "90d", Actions: map[string]interface{}{"delete": map[string]interface{}{}}},
		}},
	}

	t.Run("set", func(t *testing.T) {
		assert.NoError(t, ILM.Set(name, record))
	})

	t.Run("get", func(t *testing.T) {
		got, err := ILM.Get(name)
		require.NoError(t, err)
		assert.Equal(t, name, got.Name, "the name is taken from the key")
		assert.Equal(t, int64(1), got.Version)
		assert.Equal(t, "7d", got.Policy.Phases["warm"].MinAge)
		assert.Equal(t, "30d", got.Policy.Phases["cold"].MinAge)
		assert.NotNil(t, got.Policy.Phases["delete"].Actions)
	})

	t.Run("list", func(t *testing.T) {
		got, err := ILM.List(0, 0)
		require.NoError(t, err)
		found := false
		for _, p := range got {
			if p.Name == name {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("delete", func(t *testing.T) {
		assert.NoError(t, ILM.Delete(name))
		_, err := ILM.Get(name)
		assert.Error(t, err)
	})
}
