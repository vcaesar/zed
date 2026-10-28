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
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/routes"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

var (
	clusterEngine     *gin.Engine
	clusterEngineOnce sync.Once
)

// clusterServer registers only the cluster routes, the shared engine of
// init.go is built from SetRoutes alone.
func clusterServer() *gin.Engine {
	clusterEngineOnce.Do(func() {
		gin.SetMode(gin.ReleaseMode)
		clusterEngine = gin.New()
		clusterEngine.Use(gin.Recovery())
		routes.SetClusterRoutes(clusterEngine)
	})
	return clusterEngine
}

func clusterRequest(t *testing.T, method, api string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest(method, api, nil)
	require.NoError(t, err)
	req.SetBasicAuth(username, password)
	w := httptest.NewRecorder()
	clusterServer().ServeHTTP(w, req)
	return w
}

func TestApiCluster(t *testing.T) {
	t.Run("GET /api/cluster/nodes", func(t *testing.T) {
		resp := clusterRequest(t, http.MethodGet, "/api/cluster/nodes")
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

		data := struct {
			Enabled bool `json:"enabled"`
			Nodes   []struct {
				ID       string `json:"id"`
				Alive    bool   `json:"alive"`
				IsLeader bool   `json:"is_leader"`
			} `json:"nodes"`
		}{}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &data))

		assert.False(t, data.Enabled, "clustering is off in tests")
		require.Len(t, data.Nodes, 1, "a single process reports one node")
		assert.NotEmpty(t, data.Nodes[0].ID)
		assert.True(t, data.Nodes[0].Alive)
		assert.True(t, data.Nodes[0].IsLeader)
	})

	t.Run("GET /api/cluster/partitions", func(t *testing.T) {
		resp := clusterRequest(t, http.MethodGet, "/api/cluster/partitions?index="+indexName)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

		data := struct {
			Owners []struct {
				Index string `json:"index"`
				Local bool   `json:"local"`
			} `json:"owners"`
		}{}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &data))
		require.Len(t, data.Owners, 1)
		assert.Equal(t, indexName, data.Owners[0].Index)
		assert.True(t, data.Owners[0].Local)
	})

	t.Run("GET /api/_internal/indexes", func(t *testing.T) {
		resp := clusterRequest(t, http.MethodGet, "/api/_internal/indexes")
		assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	})

	t.Run("unauthenticated request is rejected", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "/api/cluster/nodes", nil)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		clusterServer().ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
