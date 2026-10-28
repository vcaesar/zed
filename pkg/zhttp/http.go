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
	"io"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

const jsonContentType = "application/json; charset=utf-8"

// encodeJSON renders obj, indented with four spaces like gin when asked.
func encodeJSON(obj interface{}, indent bool) ([]byte, error) {
	if indent {
		return json.MarshalIndent(obj, "", "    ")
	}
	return json.Marshal(obj)
}

// HTTPContext is a Context over net/http. The gin backend serves requests
// with it and handler tests build one with NewHTTPContext.
type HTTPContext struct {
	state
	Request *http.Request
	writer  *responseWriter
}

var _ Context = (*HTTPContext)(nil)

// NewHTTPContext returns a context without handlers for w and r.
func NewHTTPContext(w http.ResponseWriter, r *http.Request) *HTTPContext {
	return newHTTPContext(w, r, nil, nil, "")
}

func newHTTPContext(w http.ResponseWriter, r *http.Request, handlers []HandlerFunc, params []Param, fullPath string) *HTTPContext {
	return &HTTPContext{
		state:   newState(handlers, params, fullPath),
		Request: r,
		writer:  &responseWriter{ResponseWriter: w, status: http.StatusOK},
	}
}

func (c *HTTPContext) Method() string { return c.Request.Method }

func (c *HTTPContext) Path() string { return c.Request.URL.Path }

func (c *HTTPContext) RequestURI() string { return c.Request.RequestURI }

func (c *HTTPContext) Query(key string) string {
	v, _ := c.GetQuery(key)
	return v
}

func (c *HTTPContext) DefaultQuery(key, def string) string {
	if v, ok := c.GetQuery(key); ok {
		return v
	}
	return def
}

func (c *HTTPContext) GetQuery(key string) (string, bool) {
	values, ok := c.Request.URL.Query()[key]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

func (c *HTTPContext) GetHeader(key string) string { return c.Request.Header.Get(key) }

func (c *HTTPContext) BasicAuth() (string, string, bool) { return c.Request.BasicAuth() }

func (c *HTTPContext) Body() io.ReadCloser {
	if c.Request.Body == nil {
		return http.NoBody
	}
	return c.Request.Body
}

func (c *HTTPContext) SetBody(body []byte) {
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
}

func (c *HTTPContext) Context() context.Context { return c.Request.Context() }

func (c *HTTPContext) Header(key, value string) { c.writer.Header().Set(key, value) }

func (c *HTTPContext) Status(code int) { c.writer.WriteHeader(code) }

func (c *HTTPContext) JSON(code int, obj interface{}) { c.renderJSON(code, obj, false) }

func (c *HTTPContext) IndentedJSON(code int, obj interface{}) { c.renderJSON(code, obj, true) }

func (c *HTTPContext) renderJSON(code int, obj interface{}, indent bool) {
	data, err := encodeJSON(obj, indent)
	if err != nil {
		log.Error().Err(err).Msg("http: encode json response")
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(code, jsonContentType, data)
}

func (c *HTTPContext) Data(code int, contentType string, data []byte) {
	c.Header("Content-Type", contentType)
	c.writer.WriteHeader(code)
	if _, err := c.writer.Write(data); err != nil {
		log.Warn().Err(err).Msg("http: write response")
	}
}

func (c *HTTPContext) Redirect(code int, location string) {
	http.Redirect(c.writer, c.Request, location, code)
}

func (c *HTTPContext) ResponseStatus() int { return c.writer.status }

func (c *HTTPContext) ResponseSize() int { return c.writer.size }

func (c *HTTPContext) ServeHTTP(h http.Handler) { h.ServeHTTP(c.writer, c.Request) }

func (c *HTTPContext) Next() { c.next(c) }

func (c *HTTPContext) NextBuffered() func() error {
	out, status, size := c.writer.ResponseWriter, c.writer.status, c.writer.size
	held := &bufferedWriter{ResponseWriter: out}
	c.writer.ResponseWriter = held
	c.Next()
	c.writer.ResponseWriter, c.writer.status, c.writer.size = out, status, size
	return func() error { return held.flush(c.writer) }
}

func (c *HTTPContext) AbortWithStatus(code int) {
	c.Abort()
	c.Status(code)
}

func (c *HTTPContext) AbortWithStatusJSON(code int, obj interface{}) {
	c.Abort()
	c.JSON(code, obj)
}

// responseWriter records the status and size of a response.
type responseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (w *responseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.size += n
	return n, err
}

// Unwrap lets http.ResponseController reach the connection's writer.
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// bufferedWriter keeps a response in memory until it is flushed.
type bufferedWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *bufferedWriter) Write(data []byte) (int, error) { return w.body.Write(data) }

func (w *bufferedWriter) WriteHeader(status int) { w.status = status }

func (w *bufferedWriter) flush(to http.ResponseWriter) error {
	if w.status != 0 {
		to.WriteHeader(w.status)
	}
	if w.body.Len() == 0 {
		return nil
	}
	_, err := to.Write(w.body.Bytes())
	return err
}
