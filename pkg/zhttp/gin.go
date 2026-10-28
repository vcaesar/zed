//go:build gin

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
	"errors"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
)

// TLSNextProtos are the ALPN protocols the server speaks.
var TLSNextProtos = []string{"h2", "http/1.1"}

// Backend names the server the binary was built with.
const Backend = "gin"

type ginBackend struct {
	engine *gin.Engine
	server *http.Server
}

func newBackend(e *Engine) backend {
	gin.SetMode(gin.ReleaseMode)
	g := gin.New()
	g.NoRoute(func(gc *gin.Context) {
		c := newHTTPContext(gc.Writer, gc.Request, e.noRouteChain(), nil, "")
		c.writer.status = gc.Writer.Status() // gin answers 404 unless the chain writes
		c.run()
	})
	return &ginBackend{engine: g, server: &http.Server{Handler: g}}
}

func (b *ginBackend) handle(method, path string, handlers []HandlerFunc) {
	b.engine.Handle(method, path, func(gc *gin.Context) {
		params := make([]Param, len(gc.Params))
		for i, p := range gc.Params {
			params[i] = Param{Key: p.Key, Value: p.Value}
		}
		newHTTPContext(gc.Writer, gc.Request, handlers, params, gc.FullPath()).run()
	})
}

func (b *ginBackend) handler() http.Handler { return b.engine }

func (b *ginBackend) serve(ln net.Listener) error {
	if err := b.server.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (b *ginBackend) shutdown(ctx context.Context) error { return b.server.Shutdown(ctx) }

func (b *ginBackend) close() error { return b.server.Close() }

// run serves the request with the handler chain.
func (c *HTTPContext) run() {
	defer func() {
		if err := recover(); err != nil {
			recovered(c, err)
		}
	}()
	c.Next()
}
