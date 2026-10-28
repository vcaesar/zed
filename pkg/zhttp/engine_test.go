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

package zhttp

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests run against the backend of the build: gin by default,
// fiber with `-tags fiber`.

func do(e *Engine, method, target string, body io.Reader) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(method, target, body))
	return w
}

func TestEngineChain(t *testing.T) {
	e := New()
	var order []string
	e.Use(func(c Context) {
		order = append(order, "first")
		c.Header("X-First", "1") // returns without Next: the chain goes on
	})
	e.Use(func(c Context) {
		order = append(order, "before")
		c.Next()
		order = append(order, "after")
	})
	e.GET("/chain/:target", func(c Context) {
		order = append(order, "handler")
		c.JSON(http.StatusOK, H{"target": c.Param("target"), "route": c.FullPath()})
	})
	e.Use(func(c Context) { order = append(order, "late") }) // only for later routes and NoRoute

	w := do(e, http.MethodGet, "/chain/logs", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "1", w.Header().Get("X-First"))
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"target":"logs","route":"/chain/:target"}`, w.Body.String())
	assert.Equal(t, []string{"first", "before", "handler", "after"}, order)

	order = nil
	w = do(e, http.MethodGet, "/missing", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, []string{"first", "before", "late", "after"}, order)
}

func TestEngineAbort(t *testing.T) {
	e := New()
	reached := false
	e.POST("/guarded", func(c Context) {
		c.AbortWithStatusJSON(http.StatusForbidden, H{"error": "no"})
		assert.True(t, c.IsAborted())
	}, func(c Context) { reached = true })

	w := do(e, http.MethodPost, "/guarded", nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.JSONEq(t, `{"error":"no"}`, w.Body.String())
	assert.False(t, reached)
}

func TestEngineNoRoute(t *testing.T) {
	e := New()
	e.GET("/known", func(c Context) { c.Status(http.StatusNoContent) })
	e.NoRoute(func(c Context) {
		c.Data(http.StatusNotFound, "text/plain", []byte("nothing at "+c.Path()))
		assert.Empty(t, c.FullPath())
	})

	w := do(e, http.MethodGet, "/unknown", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "nothing at /unknown", w.Body.String())
	// another method on a known path is not a route either
	assert.Equal(t, http.StatusNotFound, do(e, http.MethodDelete, "/known", nil).Code)
	assert.Equal(t, http.StatusNoContent, do(e, http.MethodGet, "/known", nil).Code)
}

func TestEngineRequest(t *testing.T) {
	e := New()
	e.Use(func(c Context) {
		if c.Param("target") == "alias" {
			c.SetParam("target", "a,b")
		}
		c.Set("seen", true)
	})
	e.PUT("/req/:target/_doc/:id", func(c Context) {
		user, pass, ok := c.BasicAuth()
		require.True(t, ok)
		body, err := io.ReadAll(c.Body())
		require.NoError(t, err)
		seen, _ := c.Get("seen")
		_, hasFlag := c.GetQuery("flag")
		_, hasMissing := c.GetQuery("missing")
		c.IndentedJSON(http.StatusCreated, H{
			"method":  c.Method(),
			"path":    c.Path(),
			"uri":     c.RequestURI(),
			"target":  c.Param("target"),
			"id":      c.Param("id"),
			"q":       c.Query("q"),
			"def":     c.DefaultQuery("missing", "dflt"),
			"flag":    hasFlag,
			"missing": hasMissing,
			"header":  c.GetHeader("X-Test"),
			"auth":    user + ":" + pass,
			"body":    string(body),
			"seen":    seen,
		})
	})

	for _, tt := range []struct{ target, want string }{
		// want is the decoded target the handler sees after the alias rewrite
		{"logs", "logs"},
		{"alias", "a,b"},
		{"logs-%2A", "logs-*"}, // routed on the decoded path like gin
	} {
		t.Run(tt.target, func(t *testing.T) {
			path, err := url.PathUnescape(tt.target)
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPut, "/req/"+tt.target+"/_doc/7?q=x&flag", strings.NewReader("payload"))
			req.SetBasicAuth("admin", "secret")
			req.Header.Set("X-Test", "yes")
			w := httptest.NewRecorder()
			e.ServeHTTP(w, req)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "\n    \"") // four space indent
			assert.JSONEq(t, `{"method":"PUT","path":"/req/`+path+`/_doc/7","uri":"/req/`+tt.target+`/_doc/7?q=x&flag",
				"target":"`+tt.want+`","id":"7","q":"x","def":"dflt","flag":true,"missing":false,"header":"yes",
				"auth":"admin:secret","body":"payload","seen":true}`, w.Body.String())
		})
	}
}

func TestEngineCaseSensitive(t *testing.T) {
	e := New()
	e.GET("/api/index", func(c Context) { c.Status(http.StatusOK) })
	assert.Equal(t, http.StatusOK, do(e, http.MethodGet, "/api/index", nil).Code)
	assert.Equal(t, http.StatusNotFound, do(e, http.MethodGet, "/API/index", nil).Code)
}

func TestEngineSetBody(t *testing.T) {
	e := New()
	e.POST("/body", func(c Context) {
		first, err := io.ReadAll(c.Body())
		require.NoError(t, err)
		c.SetBody(append(first, '!'))
	}, func(c Context) {
		body, err := io.ReadAll(c.Body())
		require.NoError(t, err)
		c.Data(http.StatusOK, "text/plain", body)
	})
	assert.Equal(t, "hi!", do(e, http.MethodPost, "/body", strings.NewReader("hi")).Body.String())
}

func TestEngineNextBuffered(t *testing.T) {
	e := New()
	e.Use(func(c Context) {
		flush := c.NextBuffered()
		if c.GetHeader("X-Reject") != "" {
			c.Abort()
			c.JSON(http.StatusConflict, H{"error": "moved"})
			return
		}
		require.NoError(t, flush())
	})
	e.POST("/write", func(c Context) {
		c.JSON(http.StatusCreated, H{"written": true})
	})

	w := do(e, http.MethodPost, "/write", nil)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.JSONEq(t, `{"written":true}`, w.Body.String())

	req := httptest.NewRequest(http.MethodPost, "/write", nil)
	req.Header.Set("X-Reject", "1")
	w = httptest.NewRecorder()
	e.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.JSONEq(t, `{"error":"moved"}`, w.Body.String())
}

func TestEngineResponse(t *testing.T) {
	e := New()
	var status, size int
	e.Use(func(c Context) {
		c.Next()
		status, size = c.ResponseStatus(), c.ResponseSize()
	})
	e.GET("/data", func(c Context) { c.Data(http.StatusAccepted, "text/plain", []byte("12345")) })
	e.GET("/redirect", func(c Context) { c.Redirect(http.StatusMovedPermanently, "/data") })
	e.GET("/wrapped", func(c Context) {
		c.ServeHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Wrapped", r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
			_, err := w.Write([]byte("tea"))
			assert.NoError(t, err)
		}))
	})
	e.GET("/panic", func(c Context) { panic("boom") })
	e.HEAD("/head", func(c Context) { c.Header("X-Head", "1") })

	w := do(e, http.MethodGet, "/data", nil)
	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Equal(t, "12345", w.Body.String())
	assert.Equal(t, http.StatusAccepted, status)
	assert.Equal(t, 5, size)

	w = do(e, http.MethodGet, "/redirect", nil)
	assert.Equal(t, http.StatusMovedPermanently, w.Code)
	assert.Equal(t, "/data", w.Header().Get("Location"))

	w = do(e, http.MethodGet, "/wrapped", nil)
	assert.Equal(t, http.StatusTeapot, w.Code)
	assert.Equal(t, "tea", w.Body.String())
	assert.Equal(t, "/wrapped", w.Header().Get("X-Wrapped"))

	assert.Equal(t, http.StatusInternalServerError, do(e, http.MethodGet, "/panic", nil).Code)

	w = do(e, http.MethodHead, "/head", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "1", w.Header().Get("X-Head"))
}

func TestEngineServe(t *testing.T) {
	e := New()
	e.GET("/ping", func(c Context) { c.JSON(http.StatusOK, H{"backend": Backend}) })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	served := make(chan error, 1)
	go func() { served <- e.Serve(ln) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/ping")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"backend":"`+Backend+`"}`, string(body))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, e.Shutdown(ctx))
	select {
	case err := <-served:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}
}

func TestEngineClose(t *testing.T) {
	e := New()
	e.GET("/ping", func(c Context) { c.Status(http.StatusOK) })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	served := make(chan error, 1)
	go func() { served <- e.Serve(ln) }()
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + ln.Addr().String() + "/ping")
		if err != nil {
			return false
		}
		return resp.Body.Close() == nil && resp.StatusCode == http.StatusOK
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, e.Close())
	select {
	case err := <-served:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after Close")
	}
}
