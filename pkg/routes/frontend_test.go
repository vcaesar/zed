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
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
)

func TestFrontendRoutes(t *testing.T) {
	front, err := zedsearch.GetFrontendAssets()
	require.NoError(t, err)
	oldSwagger := config.Global.ZedSwaggerEnable
	t.Cleanup(func() { config.Global.ZedSwaggerEnable = oldSwagger })
	config.Global.ZedSwaggerEnable = true
	r := zhttp.New()
	SetRoutes(r)

	tests := []struct {
		method, path string
		api, web     int
	}{
		{http.MethodGet, "/", http.StatusOK, http.StatusFound},
		{http.MethodGet, "/version", http.StatusOK, http.StatusOK},
		{http.MethodGet, "/healthz", http.StatusOK, http.StatusOK},
		{http.MethodGet, "/api/index", http.StatusUnauthorized, http.StatusUnauthorized},
		{http.MethodGet, "/es/_cluster/health", http.StatusUnauthorized, http.StatusUnauthorized},
		{http.MethodGet, "/ui/", http.StatusNotFound, http.StatusOK},
		{http.MethodGet, "/ui/search?q=test", http.StatusNotFound, http.StatusOK},
		{http.MethodGet, "/ui/assets/missing.js", http.StatusNotFound, http.StatusNotFound},
		{http.MethodPost, "/ui/search", http.StatusNotFound, http.StatusNotFound},
		{http.MethodGet, "/swagger", http.StatusNotFound, http.StatusMovedPermanently},
		{http.MethodGet, "/swagger/index.html", http.StatusNotFound, http.StatusOK},
		{http.MethodGet, "/swagger/doc.json", http.StatusNotFound, http.StatusOK},
		{http.MethodGet, "/missing", http.StatusNotFound, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			r.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
			want := tt.api
			if front != nil {
				want = tt.web
			}
			assert.Equal(t, want, response.Code)
			if tt.path == "/" {
				if front == nil {
					assert.Empty(t, response.Header().Get("Location"))
					assert.Contains(t, response.Body.String(), `"version"`)
				} else {
					assert.Equal(t, "./ui/", response.Header().Get("Location"))
				}
			}
			if tt.path == "/ui/search?q=test" && front != nil {
				assert.Contains(t, response.Body.String(), `id="root"`)
			}
		})
	}
}
