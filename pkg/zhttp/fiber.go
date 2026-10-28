//go:build fiber

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
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/config"
)

// TLSNextProtos are the ALPN protocols the server speaks.
var TLSNextProtos = []string{"http/1.1"}

// Backend names the server the binary was built with.
const Backend = "fiber"

// notFoundBody answers unmatched requests, the same text as gin.
const notFoundBody = "404 page not found"

type fiberBackend struct {
	app       *fiber.App
	engine    *Engine
	once      sync.Once
	netHandle http.Handler
}

func newBackend(e *Engine) backend {
	app := fiber.New(fiber.Config{
		// handlers keep params and bodies beyond the request, e.g. index names
		Immutable: true,
		// match gin: case sensitive, routed on the decoded path
		CaseSensitive: true,
		UnescapePath:  true,
		BodyLimit:     config.Global.ServerBodyLimit,
	})
	return &fiberBackend{app: app, engine: e}
}

func (b *fiberBackend) handle(method, path string, handlers []HandlerFunc) {
	// gin names its catch-all (`*any`), fiber does not
	if i := strings.IndexByte(path, '*'); i >= 0 {
		path = path[:i+1]
	}
	b.app.Add([]string{method}, path, func(fc fiber.Ctx) error {
		route := fc.Route()
		params := make([]Param, len(route.Params))
		for i, key := range route.Params {
			params[i] = Param{Key: key, Value: fc.Params(key)}
		}
		newFiberContext(fc, handlers, params, route.Path).run()
		return nil
	})
}

// ready returns the fiber app once every route is in: the NoRoute chain is
// a catch-all, it has to be registered last.
func (b *fiberBackend) ready() *fiber.App {
	b.once.Do(func() {
		b.app.Use(func(fc fiber.Ctx) error {
			// like gin: 404 unless the NoRoute chain answers otherwise
			fc.Status(http.StatusNotFound)
			newFiberContext(fc, b.engine.noRouteChain(), nil, "").run()
			if fc.Response().StatusCode() == http.StatusNotFound && len(fc.Response().Body()) == 0 {
				fc.Response().SetBodyString(notFoundBody)
			}
			return nil
		})
		b.netHandle = adaptor.FiberApp(b.app)
	})
	return b.app
}

func (b *fiberBackend) handler() http.Handler {
	b.ready()
	return b.netHandle
}

func (b *fiberBackend) serve(ln net.Listener) error {
	return b.ready().Listener(ln, fiber.ListenConfig{DisableStartupMessage: true})
}

func (b *fiberBackend) shutdown(ctx context.Context) error { return b.app.ShutdownWithContext(ctx) }

func (b *fiberBackend) close() error {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.app.ShutdownWithContext(ctx); !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// fiberContext is a Context over a fiber request.
type fiberContext struct {
	state
	fc fiber.Ctx
}

func newFiberContext(fc fiber.Ctx, handlers []HandlerFunc, params []Param, fullPath string) *fiberContext {
	return &fiberContext{state: newState(handlers, params, fullPath), fc: fc}
}

func (c *fiberContext) run() {
	defer func() {
		if err := recover(); err != nil {
			c.fc.Response().ResetBody()
			recovered(c, err)
		}
	}()
	c.Next()
}

func (c *fiberContext) Method() string { return c.fc.Method() }

func (c *fiberContext) Path() string { return c.fc.Path() }

func (c *fiberContext) RequestURI() string { return c.fc.OriginalURL() }

func (c *fiberContext) Query(key string) string { return c.fc.Query(key) }

func (c *fiberContext) DefaultQuery(key, def string) string {
	if v, ok := c.GetQuery(key); ok {
		return v
	}
	return def
}

func (c *fiberContext) GetQuery(key string) (string, bool) {
	if !c.fc.RequestCtx().QueryArgs().Has(key) {
		return "", false
	}
	return c.fc.Query(key), true
}

func (c *fiberContext) GetHeader(key string) string { return c.fc.Get(key) }

func (c *fiberContext) BasicAuth() (string, string, bool) {
	return basicAuth(c.fc.Get("Authorization"))
}

// Body is the raw body, like net/http no Content-Encoding is undone.
func (c *fiberContext) Body() io.ReadCloser { return io.NopCloser(bytes.NewReader(c.fc.BodyRaw())) }

func (c *fiberContext) SetBody(body []byte) { c.fc.Request().SetBody(body) }

func (c *fiberContext) Context() context.Context { return c.fc.Context() }

func (c *fiberContext) Header(key, value string) { c.fc.Set(key, value) }

func (c *fiberContext) Status(code int) { c.fc.Status(code) }

func (c *fiberContext) JSON(code int, obj interface{}) { c.renderJSON(code, obj, false) }

func (c *fiberContext) IndentedJSON(code int, obj interface{}) { c.renderJSON(code, obj, true) }

func (c *fiberContext) renderJSON(code int, obj interface{}, indent bool) {
	data, err := encodeJSON(obj, indent)
	if err != nil {
		log.Error().Err(err).Msg("http: encode json response")
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(code, jsonContentType, data)
}

func (c *fiberContext) Data(code int, contentType string, data []byte) {
	c.fc.Set("Content-Type", contentType)
	c.fc.Status(code)
	c.fc.Response().SetBody(data)
}

func (c *fiberContext) Redirect(code int, location string) {
	c.fc.Set("Location", location)
	c.fc.Status(code)
}

func (c *fiberContext) ResponseStatus() int { return c.fc.Response().StatusCode() }

func (c *fiberContext) ResponseSize() int { return len(c.fc.Response().Body()) }

// ServeHTTP runs h with a context of its own: the adaptor's request context
// is the pooled fasthttp ctx, which work h leaves behind (a proxy's
// transport) must not touch once the request is recycled.
func (c *fiberContext) ServeHTTP(h http.Handler) {
	ctx, cancel := context.WithCancel(c.Context())
	defer cancel()
	detached := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(ctx))
	})
	if err := adaptor.HTTPHandler(detached)(c.fc); err != nil {
		log.Error().Err(err).Str("path", c.Path()).Msg("http: net/http handler")
		c.Status(fiber.ErrNotFound.Code)
	}
}

func (c *fiberContext) Next() { c.next(c) }

// NextBuffered needs no buffer: fiber sends the response after the chain.
func (c *fiberContext) NextBuffered() func() error {
	c.Next()
	return func() error { return nil }
}

func (c *fiberContext) AbortWithStatus(code int) {
	c.Abort()
	c.Status(code)
}

func (c *fiberContext) AbortWithStatusJSON(code int, obj interface{}) {
	c.Abort()
	c.JSON(code, obj)
}

// basicAuth parses an Authorization header the way net/http does.
func basicAuth(authorization string) (string, string, bool) {
	r := http.Request{Header: http.Header{"Authorization": {authorization}}}
	return r.BasicAuth()
}
