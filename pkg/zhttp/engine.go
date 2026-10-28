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
	"net"
	"net/http"
	"runtime/debug"

	"github.com/rs/zerolog/log"
)

// Engine is the router. Like gin, a route captures the middlewares added
// with Use before it, while the NoRoute chain sees all of them.
type Engine struct {
	middlewares []HandlerFunc
	noRoute     []HandlerFunc
	backend     backend
}

// backend is the server a build runs on: gin, or fiber with `-tags fiber`.
type backend interface {
	handle(method, path string, handlers []HandlerFunc)
	handler() http.Handler
	serve(ln net.Listener) error
	shutdown(ctx context.Context) error
	close() error
}

// New returns an engine without routes.
func New() *Engine {
	e := &Engine{}
	e.backend = newBackend(e)
	return e
}

// Use appends global middlewares.
func (e *Engine) Use(handlers ...HandlerFunc) {
	e.middlewares = append(e.middlewares, handlers...)
}

// Handle registers a route.
func (e *Engine) Handle(method, path string, handlers ...HandlerFunc) {
	chain := make([]HandlerFunc, 0, len(e.middlewares)+len(handlers))
	chain = append(chain, e.middlewares...)
	chain = append(chain, handlers...)
	e.backend.handle(method, path, chain)
}

func (e *Engine) GET(path string, handlers ...HandlerFunc) {
	e.Handle(http.MethodGet, path, handlers...)
}

func (e *Engine) HEAD(path string, handlers ...HandlerFunc) {
	e.Handle(http.MethodHead, path, handlers...)
}

func (e *Engine) POST(path string, handlers ...HandlerFunc) {
	e.Handle(http.MethodPost, path, handlers...)
}

func (e *Engine) PUT(path string, handlers ...HandlerFunc) {
	e.Handle(http.MethodPut, path, handlers...)
}

func (e *Engine) DELETE(path string, handlers ...HandlerFunc) {
	e.Handle(http.MethodDelete, path, handlers...)
}

// NoRoute sets the handlers for requests no route matched.
func (e *Engine) NoRoute(handlers ...HandlerFunc) {
	e.noRoute = handlers
}

func (e *Engine) noRouteChain() []HandlerFunc {
	chain := make([]HandlerFunc, 0, len(e.middlewares)+len(e.noRoute))
	chain = append(chain, e.middlewares...)
	return append(chain, e.noRoute...)
}

// ServeHTTP makes the engine a net/http handler, used by tests.
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.backend.handler().ServeHTTP(w, r)
}

// Serve accepts connections on ln until Shutdown or Close, which make it
// return nil.
func (e *Engine) Serve(ln net.Listener) error {
	return e.backend.serve(ln)
}

// Shutdown stops accepting connections and waits for the active ones.
func (e *Engine) Shutdown(ctx context.Context) error {
	return e.backend.shutdown(ctx)
}

// Close stops the server without waiting for active connections.
func (e *Engine) Close() error {
	return e.backend.close()
}

// recovered turns a handler panic into a 500, like gin.Recovery.
func recovered(c Context, err interface{}) {
	log.Error().Interface("panic", err).Str("method", c.Method()).Str("path", c.Path()).
		Bytes("stack", debug.Stack()).Msg("http: handler panic")
	c.AbortWithStatus(http.StatusInternalServerError)
}
