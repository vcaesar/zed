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

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/bluge/directory"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/routes"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

var (
	tierEngine *gin.Engine
	tierOnce   sync.Once
)

// tierServer is a full engine with the tier routes and the read-only guard in
// front of the regular routes, the order SetTierRoutes requires.
func tierServer() *gin.Engine {
	tierOnce.Do(func() {
		gin.SetMode(gin.ReleaseMode)
		tierEngine = gin.New()
		tierEngine.Use(gin.Recovery())
		routes.SetTierRoutes(tierEngine)
		routes.SetRoutes(tierEngine)
	})
	return tierEngine
}

func tierRequest(method, api, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	var req *http.Request
	if reader != nil {
		req, _ = http.NewRequest(method, api, reader)
	} else {
		req, _ = http.NewRequest(method, api, nil)
	}
	req.SetBasicAuth(username, password)
	w := httptest.NewRecorder()
	tierServer().ServeHTTP(w, req)
	return w
}

func TestAPIILM(t *testing.T) {
	policy := "TestAPIILM.policy_1"
	body := `{"policy":{"phases":{"warm":{"min_age":"7d"},"cold":{"min_age":"30d"},"delete":{"min_age":"90d"}}}}`

	t.Run("PUT /es/_ilm/policy/:name", func(t *testing.T) {
		resp := tierRequest("PUT", "/es/_ilm/policy/"+policy, body)
		assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	})

	t.Run("GET /es/_ilm/policy/:name", func(t *testing.T) {
		resp := tierRequest("GET", "/es/_ilm/policy/"+policy, "")
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

		data := map[string]interface{}{}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &data))
		entry, ok := data[policy].(map[string]interface{})
		require.True(t, ok, resp.Body.String())
		phases := entry["policy"].(map[string]interface{})["phases"].(map[string]interface{})
		assert.Equal(t, "7d", phases["warm"].(map[string]interface{})["min_age"])
		assert.Equal(t, "30d", phases["cold"].(map[string]interface{})["min_age"])
	})

	t.Run("GET /es/_ilm/policy", func(t *testing.T) {
		resp := tierRequest("GET", "/es/_ilm/policy", "")
		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Contains(t, resp.Body.String(), policy)
	})

	t.Run("PUT an invalid policy", func(t *testing.T) {
		resp := tierRequest("PUT", "/es/_ilm/policy/TestAPIILM.policy_bad", `{"policy":{"phases":{"warm":{"min_age":"soon"}}}}`)
		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("GET a missing policy", func(t *testing.T) {
		resp := tierRequest("GET", "/es/_ilm/policy/TestAPIILM.policy_missing", "")
		assert.Equal(t, http.StatusNotFound, resp.Code)
	})

	t.Run("DELETE /es/_ilm/policy/:name", func(t *testing.T) {
		resp := tierRequest("DELETE", "/es/_ilm/policy/"+policy, "")
		assert.Equal(t, http.StatusOK, resp.Code)
		resp = tierRequest("GET", "/es/_ilm/policy/"+policy, "")
		assert.Equal(t, http.StatusNotFound, resp.Code)
	})

	t.Run("unauthorized", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/es/_ilm/policy", nil)
		w := httptest.NewRecorder()
		tierServer().ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestAPITier(t *testing.T) {
	index := "TestAPITier.index_1"
	policy := "TestAPITier.policy_1"

	t.Run("prepare", func(t *testing.T) {
		resp := tierRequest("PUT", "/api/index/"+index, `{"name":"`+index+`","storage_type":"disk","shard_num":1}`)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		resp = tierRequest("PUT", "/es/_ilm/policy/"+policy, `{"policy":{"phases":{"warm":{"min_age":"7d"}}}}`)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	})
	t.Cleanup(func() {
		tierRequest("DELETE", "/api/index/"+index, "")
		tierRequest("DELETE", "/es/_ilm/policy/"+policy, "")
	})

	t.Run("GET /api/index/:target/_tier", func(t *testing.T) {
		resp := tierRequest("GET", "/api/index/"+index+"/_tier", "")
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		assert.Contains(t, resp.Body.String(), `"tier":"hot"`)
	})

	t.Run("PUT an invalid tier", func(t *testing.T) {
		resp := tierRequest("PUT", "/api/index/"+index+"/_tier", `{"tier":"frozen"}`)
		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("attach a lifecycle policy through _settings", func(t *testing.T) {
		resp := tierRequest("PUT", "/api/"+index+"/_settings", `{"lifecycle":{"policy":"`+policy+`"}}`)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

		resp = tierRequest("GET", "/api/"+index+"/_settings", "")
		require.Equal(t, http.StatusOK, resp.Code)
		assert.Contains(t, resp.Body.String(), `"policy":"`+policy+`"`)
	})

	t.Run("GET /es/:target/_ilm/explain", func(t *testing.T) {
		resp := tierRequest("GET", "/es/"+index+"/_ilm/explain", "")
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

		data := map[string]interface{}{}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &data))
		indices := data["indices"].(map[string]interface{})
		explain := indices[index].(map[string]interface{})
		assert.Equal(t, true, explain["managed"])
		assert.Equal(t, "hot", explain["tier"])
		assert.Equal(t, policy, explain["policy"])
		assert.Contains(t, explain, "age")
		assert.Contains(t, explain, "action")
	})

	t.Run("explain a missing index", func(t *testing.T) {
		resp := tierRequest("GET", "/es/TestAPITier.index_missing/_ilm/explain", "")
		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})
}

// TestAPITierCold moves a real index to object storage and checks the
// cold semantics end to end: still searchable, every write rejected with 403,
// writable again after re-heating.
func TestAPITierCold(t *testing.T) {
	store := directory.NewMemoryObjectStore()
	restore, err := directory.SetTestObjectStore("s3", store)
	require.NoError(t, err)
	defer restore()

	oldBucket, oldCold := config.Global.S3.Bucket, config.Global.Tier.ColdStorageType
	config.Global.S3.Bucket, config.Global.Tier.ColdStorageType = "zed-test", "s3"
	defer func() {
		config.Global.S3.Bucket, config.Global.Tier.ColdStorageType = oldBucket, oldCold
	}()

	name := "TestAPITierCold.index_1"
	resp := tierRequest("PUT", "/api/index/"+name, `{"name":"`+name+`","storage_type":"disk","shard_num":1}`)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	t.Cleanup(func() { tierRequest("DELETE", "/api/index/"+name, "") })

	resp = tierRequest("PUT", "/es/"+name+"/_doc/1", `{"name":"hot document"}`)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.Eventually(t, func() bool {
		return tierRequest("GET", "/es/"+name+"/_doc/1", "").Code == http.StatusOK
	}, 20*time.Second, 20*time.Millisecond, "document was not indexed")

	t.Run("move to cold", func(t *testing.T) {
		resp := tierRequest("PUT", "/api/index/"+name+"/_tier", `{"tier":"cold"}`)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		assert.Positive(t, store.Len(name+"/"))
	})

	t.Run("cold is searchable", func(t *testing.T) {
		resp := tierRequest("POST", "/es/"+name+"/_search", `{"query":{"match_all":{}}}`)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		assert.Contains(t, resp.Body.String(), "hot document")
	})

	t.Run("writes are rejected with 403", func(t *testing.T) {
		for _, tt := range []struct{ method, path, body string }{
			{"PUT", "/es/" + name + "/_doc/2", `{"name":"nope"}`},
			{"POST", "/es/" + name + "/_update/1", `{"doc":{"name":"nope"}}`},
			{"DELETE", "/es/" + name + "/_doc/1", ""},
			{"POST", "/es/" + name + "/_bulk", "{\"index\":{\"_id\":\"3\"}}\n{\"name\":\"nope\"}\n"},
		} {
			t.Run(tt.method+" "+tt.path, func(t *testing.T) {
				resp := tierRequest(tt.method, tt.path, tt.body)
				assert.Equal(t, http.StatusForbidden, resp.Code)
				assert.Contains(t, resp.Body.String(), "index is cold (read-only)")
			})
		}
	})

	t.Run("re-heat makes it writable again", func(t *testing.T) {
		resp := tierRequest("PUT", "/api/index/"+name+"/_tier", `{"tier":"hot"}`)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		assert.Equal(t, 0, store.Len(name+"/"))

		resp = tierRequest("PUT", "/es/"+name+"/_doc/2", `{"name":"warm again"}`)
		assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	})
}
