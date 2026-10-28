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

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/vcaesar/zedsearch/pkg/cluster/replication"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
)

func TestClusterSecretMiddleware(t *testing.T) {
	previous := config.Global.Cluster.Secret
	t.Cleanup(func() { config.Global.Cluster.Secret = previous })

	engine := zhttp.New()
	engine.POST(replication.ReplicatePath, ClusterSecretMiddleware, func(c zhttp.Context) {
		c.JSON(http.StatusOK, zhttp.H{"ok": true})
	})
	tests := []struct {
		name   string
		secret string
		header string
		want   int
	}{
		{name: "matching secret", secret: "s3cret", header: "s3cret", want: http.StatusOK},
		{name: "wrong secret", secret: "s3cret", header: "nope", want: http.StatusUnauthorized},
		{name: "unset secret never matches", secret: "", header: "", want: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.Global.Cluster.Secret = tt.secret
			req := httptest.NewRequest(http.MethodPost, replication.ReplicatePath, nil)
			req.Header.Set(replication.SecretHeader, tt.header)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			assert.Equal(t, tt.want, w.Code)
		})
	}
}

// Without replication an untargeted write such as `_bulk` runs untouched.
func TestClusterRouteUntargetedWrite(t *testing.T) {
	env := setupClusterEnv(t)
	env.engine.POST("/api/_bulk", func(c zhttp.Context) {
		c.JSON(http.StatusOK, zhttp.H{"bulk": true})
	})
	w := env.do(httptest.NewRequest(http.MethodPost, "/api/_bulk", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "bulk")
	assert.False(t, replicated(localIndex), "replication is off in this test")
}

func TestIsBulk(t *testing.T) {
	for path, want := range map[string]bool{
		"/api/_bulk":       true,
		"/api/_bulkv2":     true,
		"/api/logs/_bulk":  true,
		"/es/_bulk":        true,
		"/es/logs/_bulk":   true,
		"/api/logs/_doc":   false,
		"/api/logs/_multi": false,
		"/es/logs/_doc/1":  false,
	} {
		assert.Equal(t, want, isBulk(path), path)
	}
}
