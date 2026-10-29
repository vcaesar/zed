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

	"github.com/vcaesar/zedsearch/pkg/cluster/replication"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/handlers/cluster"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
)

// SetClusterRoutes installs the cluster endpoints and the ownership
// routing middleware.
//
// It must run before SetRoutes: gin freezes the handler chain of a route
// when the route is registered, so ClusterRoute only covers routes added
// after this call.
func SetClusterRoutes(r *zhttp.Engine) {
	r.Use(ClusterRoute())

	// operator facing
	r.GET("/api/cluster/nodes", AuthMiddleware("cluster.Nodes"), cluster.Nodes)
	r.GET("/api/cluster/partitions", AuthMiddleware("cluster.Partitions"), cluster.Partitions)

	// node to node, the caller's credentials are forwarded unchanged
	r.POST("/api/_internal/search", AuthMiddleware("cluster.InternalSearch"), cluster.InternalSearch)
	r.GET("/api/_internal/indexes", AuthMiddleware("cluster.InternalIndexes"), cluster.InternalIndexes)

	// replication, authenticated with the cluster secret only
	r.POST(replication.ReplicatePath, ClusterSecretMiddleware, cluster.ReplicationReplicate)
	r.POST(replication.RecoverPath, ClusterSecretMiddleware, cluster.ReplicationRecover)
	r.POST(replication.SnapshotPath, ClusterSecretMiddleware, cluster.ReplicationSnapshot)
	r.GET(replication.SnapshotPath, ClusterSecretMiddleware, cluster.ReplicationSnapshot) // nodes before incremental recovery
}

// ClusterSecretMiddleware admits node to node calls carrying
// `zed_cluster_secret`. Background replication has no user credentials.
func ClusterSecretMiddleware(c zhttp.Context) {
	if !replication.CheckSecret(c.GetHeader(replication.SecretHeader), config.Global.Cluster.Secret) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, zhttp.H{"auth": "Invalid cluster secret"})
		return
	}
	c.Next()
}
