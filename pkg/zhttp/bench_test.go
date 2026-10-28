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
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// BenchmarkServe measures the backend over a real socket, the way clients
// see it: run once plain (gin) and once with -tags fiber.
func BenchmarkServe(b *testing.B) {
	e := New()
	e.Use(func(c Context) { c.Set("user", "admin") })
	e.GET("/api/:target/_doc/:id", func(c Context) {
		c.JSON(http.StatusOK, H{"_index": c.Param("target"), "_id": c.Param("id")})
	})
	big := H{"hits": strings.Repeat("x", 64<<10)}
	e.GET("/api/:target/_search", func(c Context) { c.JSON(http.StatusOK, big) })
	e.POST("/api/:target/_doc", func(c Context) {
		body, err := io.ReadAll(c.Body())
		if err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		c.JSON(http.StatusOK, H{"size": len(body)})
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	go func() {
		if err := e.Serve(ln); err != nil {
			b.Error(err)
		}
	}()
	b.Cleanup(func() {
		if err := e.Close(); err != nil {
			b.Error(err)
		}
	})

	base := "http://" + ln.Addr().String()
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 256}}
	payload := bytes.Repeat([]byte(`{"title":"zedsearch"}`), 200)

	cases := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"param_json", http.MethodGet, "/api/games/_doc/1", nil},
		{"large_json_64k", http.MethodGet, "/api/games/_search", nil},
		{"post_body_4k", http.MethodPost, "/api/games/_doc", payload},
	}
	for _, tt := range cases {
		b.Run(Backend+"/"+tt.name, func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					req, err := http.NewRequest(tt.method, base+tt.path, bytes.NewReader(tt.body))
					if err != nil {
						b.Error(err)
						return
					}
					resp, err := client.Do(req)
					if err != nil {
						b.Error(err)
						return
					}
					if _, err := io.Copy(io.Discard, resp.Body); err != nil {
						b.Error(err)
					}
					if err := resp.Body.Close(); err != nil {
						b.Error(err)
					}
					if resp.StatusCode != http.StatusOK {
						b.Errorf("status %d", resp.StatusCode)
						return
					}
				}
			})
		})
	}
}
