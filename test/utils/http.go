/* Copyright 2022 Zinc Labs Inc. and Contributors
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

package utils

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/vcaesar/zedsearch/pkg/zhttp"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

func NewContext() (*zhttp.HTTPContext, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	req := &http.Request{
		URL:    &url.URL{},
		Header: make(http.Header),
	}
	return zhttp.NewHTTPContext(w, req), w
}

func SetRequestData(c *zhttp.HTTPContext, data interface{}) {
	c.Request.Header.Set("Content-Type", "application/json;charset=utf-8")
	switch v := data.(type) {
	case string:
		c.Request.Body = io.NopCloser(bytes.NewBufferString(v))
	case map[string]interface{}:
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			return
		}
		buf := bytes.NewBuffer(jsonBytes)
		c.Request.Body = io.NopCloser(buf)
	default:
	}
}

func SetRequestURL(c *zhttp.HTTPContext, path string, params map[string]string) {
	q := c.Request.URL.Query()
	for k, v := range params {
		q.Add(k, v)
	}
	c.Request.URL.Path = path
	c.Request.URL.RawQuery = q.Encode()
}

func SetRequestParams(c *zhttp.HTTPContext, params map[string]string) {
	for k, v := range params {
		c.SetParam(k, v)
	}
}
