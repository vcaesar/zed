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

package routes

import (
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/vcaesar/zedsearch/pkg/zhttp"
)

func HTTPCacheForUI(app *zhttp.Engine) {
	app.Use(func(c zhttp.Context) {
		if c.Method() == "GET" || c.Method() == "HEAD" {
			if strings.Contains(c.RequestURI(), "/ui/assets/") {
				c.Header("cache-control", "public, max-age=2592000")
				c.Header("expires", time.Now().Add(time.Hour*24*30).Format(time.RFC1123))
				if strings.Contains(c.RequestURI(), ".js") {
					c.Header("content-type", "application/javascript")
				}
				if strings.Contains(c.RequestURI(), ".css") {
					c.Header("content-type", "text/css; charset=utf-8")
				}
			}
		}

		c.Next()
	})
}

// serveUI serves the console files under /ui/, a missing file goes to
// notFound so client side routes still load index.html.
func serveUI(front fs.FS, notFound zhttp.HandlerFunc) zhttp.HandlerFunc {
	files := http.StripPrefix("/ui/", http.FileServer(http.FS(front)))
	return func(c zhttp.Context) {
		if c.Path() == "/ui" {
			c.Redirect(http.StatusMovedPermanently, "/ui/")
			return
		}
		if name := strings.TrimPrefix(c.Path(), "/ui/"); name != "" {
			if _, err := fs.Stat(front, strings.TrimSuffix(name, "/")); err != nil {
				notFound(c)
				return
			}
		}
		c.ServeHTTP(files)
	}
}
