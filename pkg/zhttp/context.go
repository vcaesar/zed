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

// Package zhttp is the HTTP layer the handlers are written against. The
// server runs on gin by default, `-tags fiber` switches it to fiber; both run
// the same handler chains with gin's middleware semantics.
package zhttp

import (
	"context"
	"io"
	"math"
	"net/http"
)

// H is a shortcut for a JSON object.
type H map[string]interface{}

// HandlerFunc handles a request or acts as a middleware.
type HandlerFunc func(Context)

// Context is one request travelling through a handler chain.
//
// A handler that returns without calling Next or Abort lets the chain
// continue, Next runs the rest of the chain before returning, Abort stops it.
type Context interface {
	// Method is the request method.
	Method() string
	// Path is the decoded URL path.
	Path() string
	// RequestURI is the unmodified request target, path and query.
	RequestURI() string
	// FullPath is the matched route pattern, empty when no route matched.
	FullPath() string
	// Param returns a route parameter.
	Param(key string) string
	// SetParam overrides a route parameter for the rest of the chain.
	SetParam(key, value string)
	// Query returns a query string value.
	Query(key string) string
	// DefaultQuery returns a query string value or def when it is absent.
	DefaultQuery(key, def string) string
	// GetQuery returns a query string value and whether it is present.
	GetQuery(key string) (string, bool)
	// GetHeader returns a request header.
	GetHeader(key string) string
	// BasicAuth returns the credentials of the Authorization header.
	BasicAuth() (username, password string, ok bool)
	// Body returns the request body, never nil.
	Body() io.ReadCloser
	// SetBody replaces the request body for the rest of the chain.
	SetBody(body []byte)
	// Context returns the request context.
	Context() context.Context
	// Set stores a value for the rest of the chain.
	Set(key string, value interface{})
	// Get returns a value stored with Set.
	Get(key string) (interface{}, bool)

	// Header sets a response header.
	Header(key, value string)
	// Status sets the response status code.
	Status(code int)
	// JSON writes obj as JSON.
	JSON(code int, obj interface{})
	// IndentedJSON writes obj as indented JSON.
	IndentedJSON(code int, obj interface{})
	// Data writes raw bytes.
	Data(code int, contentType string, data []byte)
	// Redirect sends the client to location.
	Redirect(code int, location string)
	// ResponseStatus is the status code written so far.
	ResponseStatus() int
	// ResponseSize is the number of body bytes written so far.
	ResponseSize() int
	// ServeHTTP lets a net/http handler answer the request.
	ServeHTTP(h http.Handler)

	// Next runs the remaining handlers of the chain.
	Next()
	// NextBuffered runs the remaining handlers holding their response back.
	// Calling flush sends it, otherwise it is dropped and the caller writes
	// its own response.
	NextBuffered() (flush func() error)
	// Abort stops the chain after the current handler.
	Abort()
	// IsAborted reports whether Abort was called.
	IsAborted() bool
	// AbortWithStatus aborts and writes the status code.
	AbortWithStatus(code int)
	// AbortWithStatusJSON aborts and writes obj as JSON.
	AbortWithStatusJSON(code int, obj interface{})
}

// Param is a route parameter.
type Param struct {
	Key   string
	Value string
}

const abortIndex = math.MaxInt / 2

// state is the backend independent part of a Context.
type state struct {
	handlers []HandlerFunc
	index    int
	params   []Param
	keys     map[string]interface{}
	fullPath string
}

func newState(handlers []HandlerFunc, params []Param, fullPath string) state {
	return state{handlers: handlers, index: -1, params: params, fullPath: fullPath}
}

func (s *state) next(c Context) {
	s.index++
	for s.index < len(s.handlers) {
		s.handlers[s.index](c)
		s.index++
	}
}

func (s *state) Abort() {
	s.index = abortIndex
}

func (s *state) IsAborted() bool {
	return s.index >= abortIndex
}

func (s *state) FullPath() string {
	return s.fullPath
}

func (s *state) Param(key string) string {
	for _, p := range s.params {
		if p.Key == key {
			return p.Value
		}
	}
	return ""
}

func (s *state) SetParam(key, value string) {
	for i, p := range s.params {
		if p.Key == key {
			s.params[i].Value = value
			return
		}
	}
	s.params = append(s.params, Param{Key: key, Value: value})
}

func (s *state) Set(key string, value interface{}) {
	if s.keys == nil {
		s.keys = make(map[string]interface{})
	}
	s.keys[key] = value
}

func (s *state) Get(key string) (interface{}, bool) {
	v, ok := s.keys[key]
	return v, ok
}
