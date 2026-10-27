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
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch/pkg/bluge/directory"
	"github.com/zincsearch/zincsearch/pkg/config"
	"github.com/zincsearch/zincsearch/pkg/core"
	"github.com/zincsearch/zincsearch/test/utils"
)

// useObjectStore points the s3 backend at an in-memory store and makes it the
// cold storage type for the duration of the test.
func useObjectStore(t *testing.T) *directory.MemoryObjectStore {
	t.Helper()
	store := directory.NewMemoryObjectStore()
	restore, err := directory.SetTestObjectStore("s3", store)
	require.NoError(t, err)

	oldBucket := config.Global.S3.Bucket
	oldCold := config.Global.Tier.ColdStorageType
	config.Global.S3.Bucket = "zinc-test"
	config.Global.Tier.ColdStorageType = "s3"
	t.Cleanup(func() {
		config.Global.S3.Bucket = oldBucket
		config.Global.Tier.ColdStorageType = oldCold
		restore()
	})
	return store
}

func TestTier(t *testing.T) {
	useObjectStore(t)
	name := "TestTier.index_1"

	t.Run("create index", func(t *testing.T) {
		index, err := core.NewIndex(name, "disk", 1)
		require.NoError(t, err)
		require.NoError(t, core.StoreIndex(index))
	})
	t.Cleanup(func() { _ = core.DeleteIndex(name) })

	t.Run("get tier of a hot index", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"target": name})
		GetTier(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"tier":"hot"`)
		assert.Contains(t, w.Body.String(), `"read_only":false`)
	})

	t.Run("get tier of a missing index", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"target": "TestTier.index_missing"})
		GetTier(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("set an invalid tier", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, map[string]interface{}{"tier": "frozen"})
		utils.SetGinRequestParams(c, map[string]string{"target": name})
		SetTier(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("set a tier on a missing index", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, map[string]interface{}{"tier": "cold"})
		utils.SetGinRequestParams(c, map[string]string{"target": "TestTier.index_missing"})
		SetTier(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("bad json", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, `{"tier":}`)
		utils.SetGinRequestParams(c, map[string]string{"target": name})
		SetTier(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("move to cold and back", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, map[string]interface{}{"tier": "cold"})
		utils.SetGinRequestParams(c, map[string]string{"target": name})
		SetTier(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		c, w = utils.NewGinContext()
		utils.SetGinRequestParams(c, map[string]string{"target": name})
		GetTier(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"tier":"cold"`)
		assert.Contains(t, w.Body.String(), `"read_only":true`)
		assert.Contains(t, w.Body.String(), `"storage_type":"s3"`)

		c, w = utils.NewGinContext()
		utils.SetGinRequestData(c, map[string]interface{}{"tier": "hot"})
		utils.SetGinRequestParams(c, map[string]string{"target": name})
		SetTier(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		index, ok := core.GetIndex(name)
		require.True(t, ok)
		assert.Equal(t, core.TierHot, index.GetTier())
		assert.False(t, index.IsReadOnly())
	})
}

func TestSetSettingsLifecycleAndTier(t *testing.T) {
	useObjectStore(t)
	name := "TestSetSettingsLifecycleAndTier.index_1"

	index, err := core.NewIndex(name, "disk", 1)
	require.NoError(t, err)
	require.NoError(t, core.StoreIndex(index))
	t.Cleanup(func() { _ = core.DeleteIndex(name) })

	t.Run("attach lifecycle", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, map[string]interface{}{
			"lifecycle": map[string]interface{}{"policy": "TestSetSettings.policy", "warm_after": "7d"},
		})
		utils.SetGinRequestParams(c, map[string]string{"target": name})
		SetSettings(c)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

		settings := index.GetSettings()
		require.NotNil(t, settings.Lifecycle)
		assert.Equal(t, "TestSetSettings.policy", settings.Lifecycle.Policy)
		assert.Equal(t, "7d", settings.Lifecycle.WarmAfter)
	})

	t.Run("move tier through settings", func(t *testing.T) {
		c, w := utils.NewGinContext()
		utils.SetGinRequestData(c, map[string]interface{}{"tier": "warm"})
		utils.SetGinRequestParams(c, map[string]string{"target": name})
		SetSettings(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, core.TierWarm, index.GetTier())
		assert.False(t, index.IsReadOnly())
	})
}
