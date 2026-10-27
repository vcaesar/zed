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
	"github.com/gin-gonic/gin"

	"github.com/zincsearch/zincsearch/pkg/handlers/cluster"
)

// SetClusterRoutes installs the cluster endpoints and the ownership
// routing middleware.
//
// It must run before SetRoutes: gin freezes the handler chain of a route
// when the route is registered, so ClusterRoute only covers routes added
// after this call.
func SetClusterRoutes(r *gin.Engine) {
	r.Use(ClusterRoute())

	// operator facing
	r.GET("/api/cluster/nodes", AuthMiddleware("cluster.Nodes"), cluster.Nodes)
	r.GET("/api/cluster/partitions", AuthMiddleware("cluster.Partitions"), cluster.Partitions)

	// node to node, the caller's credentials are forwarded unchanged
	r.POST("/api/_internal/search", AuthMiddleware("cluster.InternalSearch"), cluster.InternalSearch)
	r.GET("/api/_internal/indexes", AuthMiddleware("cluster.InternalIndexes"), cluster.InternalIndexes)
}
