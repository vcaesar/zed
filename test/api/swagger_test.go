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
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch"
)

func TestSwagger(t *testing.T) {
	front, err := zincsearch.GetFrontendAssets()
	require.NoError(t, err)
	if front == nil {
		for _, path := range []string{"/swagger", "/swagger/", "/swagger/index.html", "/swagger/doc.json"} {
			assert.Equal(t, http.StatusNotFound, request(http.MethodGet, path, nil).Code, path)
		}
		return
	}
	tests := []struct {
		path        string
		code        int
		contentType string
		contains    string
	}{
		{"/swagger", http.StatusMovedPermanently, "", ""},
		{"/swagger/index.html", http.StatusOK, "text/html", `id="swagger-ui"`},
		{"/swagger/", http.StatusOK, "text/html", `id="swagger-ui"`},
		{"/swagger/doc.json", http.StatusOK, "application/json", `"/api/index"`},
		{"/swagger/swagger-ui-bundle.js.map", http.StatusNotFound, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			resp := request(http.MethodGet, tt.path, nil)
			assert.Equal(t, tt.code, resp.Code)
			assert.Contains(t, resp.Header().Get("Content-Type"), tt.contentType)
			assert.Contains(t, resp.Body.String(), tt.contains)
		})
	}

	// The page's hashed script/stylesheet are served from the console's /ui/assets/ mount.
	t.Run("assets", func(t *testing.T) {
		index := request(http.MethodGet, "/swagger/index.html", nil).Body.String()
		script := regexp.MustCompile(`<script[^>]+src="([^"]+)"`).FindStringSubmatch(index)
		style := regexp.MustCompile(`<link[^>]+rel="stylesheet"[^>]+href="([^"]+)"`).FindStringSubmatch(index)
		assert.Len(t, script, 2)
		assert.Len(t, style, 2)
		for _, url := range []string{script[1], style[1]} {
			assert.Regexp(t, `^/ui/assets/`, url)
			assert.Equal(t, http.StatusOK, request(http.MethodGet, url, nil).Code, url)
		}
		js := request(http.MethodGet, script[1], nil).Body.String()
		assert.Contains(t, js, "SwaggerUIBundle")
		assert.Contains(t, js, "Search by method, path, summary or tag")
	})
}
