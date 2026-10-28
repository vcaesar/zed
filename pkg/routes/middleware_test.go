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

	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
)

func serve(e *zhttp.Engine, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func TestCORS(t *testing.T) {
	e := zhttp.New()
	e.Use(CORS)
	e.GET("/es/", ESMiddleware, func(c zhttp.Context) { c.Status(http.StatusOK) })

	t.Run("no origin", func(t *testing.T) {
		w := serve(e, httptest.NewRequest(http.MethodGet, "/es/", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "Elasticsearch", w.Header().Get("X-elastic-product"))
	})

	t.Run("simple request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/es/", nil)
		req.Header.Set("Origin", "http://example.com")
		w := serve(e, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
		assert.Equal(t, "Content-Length", w.Header().Get("Access-Control-Expose-Headers"))
	})

	t.Run("preflight", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/index", nil)
		req.Header.Set("Origin", "http://example.com")
		w := serve(e, req)
		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "DELETE")
		assert.Equal(t, "Origin,Authorization,Content-Type", w.Header().Get("Access-Control-Allow-Headers"))
		assert.Equal(t, "43200", w.Header().Get("Access-Control-Max-Age"))
	})
}

func TestAuthMiddlewareRejects(t *testing.T) {
	e := zhttp.New()
	e.GET("/api/:target/_count", AuthMiddleware("search.Count"), func(c zhttp.Context) {
		t.Error("handler must not run")
	})

	w := serve(e, httptest.NewRequest(http.MethodGet, "/api/logs/_count", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.JSONEq(t, `{"auth":"Missing credentials"}`, w.Body.String())

	req := httptest.NewRequest(http.MethodGet, "/api/logs/_count", nil)
	req.SetBasicAuth("nobody", "wrong")
	w = serve(e, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.JSONEq(t, `{"auth":"Invalid credentials"}`, w.Body.String())
}

func TestIndexAliasMiddlewareWithoutAlias(t *testing.T) {
	e := zhttp.New()
	e.GET("/es/:target/_count", IndexAliasMiddleware, func(c zhttp.Context) {
		c.Data(http.StatusOK, "text/plain", []byte(c.Param("target")))
	})
	w := serve(e, httptest.NewRequest(http.MethodGet, "/es/TestIndexAliasMiddleware.none/_count", nil))
	assert.Equal(t, "TestIndexAliasMiddleware.none", w.Body.String())
}

func TestSetPrometheus(t *testing.T) {
	previous := config.Global.PrometheusEnable
	t.Cleanup(func() { config.Global.PrometheusEnable = previous })

	config.Global.PrometheusEnable = false
	e := zhttp.New()
	SetPrometheus(e)
	assert.Equal(t, http.StatusNotFound, serve(e, httptest.NewRequest(http.MethodGet, metricsPath, nil)).Code)

	config.Global.PrometheusEnable = true
	e = zhttp.New()
	SetPrometheus(e)
	e.GET("/healthz", func(c zhttp.Context) { c.Status(http.StatusOK) })
	core.SetMetricStatsByIndex("TestSetPrometheus.index", "docs", 3)

	assert.Equal(t, http.StatusOK, serve(e, httptest.NewRequest(http.MethodGet, "/healthz", nil)).Code)
	w := serve(e, httptest.NewRequest(http.MethodGet, metricsPath, nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `zed_requests_total{code="200",method="GET",url="/healthz"}`)
	assert.Contains(t, w.Body.String(), "zed_request_duration_seconds")
	assert.Contains(t, w.Body.String(), `zed_index_stats{field="docs",index="TestSetPrometheus.index"} 3`)
}

func TestSetPProf(t *testing.T) {
	e := zhttp.New()
	SetPProf(e)
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/cmdline", "/debug/pprof/heap?debug=1"} {
		w := serve(e, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, w.Code, path)
		assert.NotEmpty(t, w.Body.String(), path)
	}
}

func TestAccessLog(t *testing.T) {
	e := zhttp.New()
	AccessLog(e)
	e.GET("/version", func(c zhttp.Context) { c.Status(http.StatusOK) })
	w := serve(e, httptest.NewRequest(http.MethodGet, "/version", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("Zed"))
}
