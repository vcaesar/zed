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

	"github.com/vcaesar/zedsearch/pkg/zhttp"
)

// CORS allows every origin and answers preflight requests.
func CORS(c zhttp.Context) {
	if c.GetHeader("Origin") == "" {
		return
	}
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Credentials", "true")
	if c.Method() != http.MethodOptions {
		c.Header("Access-Control-Expose-Headers", "Content-Length")
		return
	}
	c.Header("Access-Control-Allow-Methods", "GET,POST,DELETE,PUT,HEAD,OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Origin,Authorization,Content-Type")
	c.Header("Access-Control-Max-Age", "43200")
	c.AbortWithStatus(http.StatusNoContent)
}
