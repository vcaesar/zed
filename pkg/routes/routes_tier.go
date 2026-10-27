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
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/zincsearch/zincsearch/pkg/core"
	"github.com/zincsearch/zincsearch/pkg/handlers/index"
)

// documentWritePaths are the route suffixes that add, change or remove
// documents. ReadOnlyGuard only has to look at those.
var documentWritePaths = []string{"/_doc", "/_create", "/_update", "/_bulk", "/_multi", "/_delete_by_query"}

// ReadOnlyGuard rejects document writes to a cold index. It has to be
// registered before the routes it protects, so SetTierRoutes runs first.
func ReadOnlyGuard(c *gin.Context) {
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
	default:
		c.Next()
		return
	}
	target := c.Param("target")
	if target == "" || !isDocumentWrite(c.FullPath()) {
		c.Next()
		return
	}
	if idx, ok := core.GetIndex(target); ok && idx.IsReadOnly() {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": core.ErrIndexReadOnly.Error()})
		return
	}
	c.Next()
}

func isDocumentWrite(path string) bool {
	for _, suffix := range documentWritePaths {
		if strings.Contains(path, suffix) {
			return true
		}
	}
	return false
}

// SetTierRoutes registers the data tier and index lifecycle endpoints together
// with the read-only guard. Call it before SetRoutes.
func SetTierRoutes(r *gin.Engine) {
	r.Use(ReadOnlyGuard)

	// data tier
	r.GET("/api/index/:target/_tier", AuthMiddleware("index.GetTier"), index.GetTier)
	r.PUT("/api/index/:target/_tier", AuthMiddleware("index.SetTier"), index.SetTier)

	// ES compatible index lifecycle management
	r.GET("/es/_ilm/policy", AuthMiddleware("index.ListILMPolicy"), ESMiddleware, index.ListILMPolicy)
	r.GET("/es/_ilm/policy/:name", AuthMiddleware("index.GetILMPolicy"), ESMiddleware, index.GetILMPolicy)
	r.PUT("/es/_ilm/policy/:name", AuthMiddleware("index.PutILMPolicy"), ESMiddleware, index.PutILMPolicy)
	r.DELETE("/es/_ilm/policy/:name", AuthMiddleware("index.DeleteILMPolicy"), ESMiddleware, index.DeleteILMPolicy)
	r.GET("/es/:target/_ilm/explain", AuthMiddleware("index.ExplainILM"), ESMiddleware, index.ExplainILM)
}
