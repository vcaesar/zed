/* Copyright 2022 Zinc Labs Inc. and Contributors
* Copyright 2026 AtomAI
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

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/handlers/auth"
	"github.com/vcaesar/zedsearch/pkg/handlers/cluster"
	"github.com/vcaesar/zedsearch/pkg/handlers/document"
	"github.com/vcaesar/zedsearch/pkg/handlers/index"
	"github.com/vcaesar/zedsearch/pkg/handlers/search"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/meta/elastic"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

// SetRoutes sets up all HTTP API endpoints that can be called by front end
func SetRoutes(r *zhttp.Engine) {
	r.Use(CORS)

	r.GET("/version", meta.GetVersion)
	r.GET("/healthz", meta.GetHealthz)

	front, err := zedsearch.GetFrontendAssets()
	if err != nil {
		log.Error().Err(err).Msg("frontend assets")
	}

	if front != nil {
		r.GET("/", meta.GUI)
	} else {
		r.GET("/", meta.GetVersion)
	}

	if front != nil && config.Global.ZedSwaggerEnable {
		swagger, err := swaggerHandler(front)
		if err != nil {
			log.Error().Err(err).Msg("swagger ui assets")
		} else {
			// the catch-all goes first: fiber's lenient routing would send /swagger/ to /swagger
			r.GET("/swagger/*any", swagger)
			r.GET("/swagger", swagger)
		}
	}

	noRoute := func(c zhttp.Context) {
		if front != nil && c.Method() == http.MethodGet && strings.HasPrefix(c.Path(), "/ui/") &&
			!strings.HasPrefix(c.Path(), "/ui/assets/") {
			index, err := fs.ReadFile(front, "index.html")
			if err != nil {
				log.Error().Err(err).Msg("read frontend index")
				c.Status(http.StatusInternalServerError)
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", index)
			return
		}

		log.Error().
			Str("method", c.Method()).
			Int("code", 404).
			Int("took", 0).
			Msg(c.RequestURI())
		c.Data(http.StatusNotFound, "text/plain; charset=utf-8", []byte("404 page not found"))
	}

	// UI
	if front != nil {
		HTTPCacheForUI(r)
		ui := serveUI(front, noRoute)
		r.GET("/ui/*filepath", ui)
		r.HEAD("/ui/*filepath", ui)
	}
	r.NoRoute(noRoute)

	// auth
	r.POST("/api/login", auth.Login)
	r.PUT("/api/account", auth.UpdateAccount)
	r.POST("/api/user", AuthMiddleware("auth.CreateUpdateUser"), auth.CreateUpdateUser)
	r.PUT("/api/user", AuthMiddleware("auth.CreateUpdateUser"), auth.CreateUpdateUser)
	r.DELETE("/api/user/:id", AuthMiddleware("auth.DeleteUser"), auth.DeleteUser)
	r.GET("/api/user", AuthMiddleware("auth.ListUser"), auth.ListUser)
	r.GET("/api/permissions", AuthMiddleware("auth.ListPermissions"), auth.ListPermissions)
	r.GET("/api/role", AuthMiddleware("auth.ListRole"), auth.ListRole)
	r.POST("/api/role", AuthMiddleware("auth.CreateUpdateRole"), auth.CreateUpdateRole)
	r.PUT("/api/role", AuthMiddleware("auth.CreateUpdateRole"), auth.CreateUpdateRole)
	r.DELETE("/api/role/:id", AuthMiddleware("auth.DeleteRole"), auth.DeleteRole)

	// index
	r.GET("/api/index", AuthMiddleware("index.List"), index.List)
	r.GET("/api/index_name", AuthMiddleware("index.IndexNameList"), index.IndexNameList)
	r.POST("/api/index", AuthMiddleware("index.Create"), index.Create)
	r.PUT("/api/index", AuthMiddleware("index.Create"), index.Create)
	r.PUT("/api/index/:target", AuthMiddleware("index.Create"), index.Create)
	r.DELETE("/api/index/:target", AuthMiddleware("index.Delete"), index.Delete)
	r.GET("/api/index/:target", AuthMiddleware("index.Get"), index.Get)
	r.HEAD("/api/index/:target", AuthMiddleware("index.Exists"), index.Exists)
	r.POST("/api/index/:target/refresh", AuthMiddleware("index.Refresh"), index.Refresh)
	// index settings
	r.GET("/api/:target/_mapping", AuthMiddleware("index.GetMapping"), index.GetMapping)
	r.PUT("/api/:target/_mapping", AuthMiddleware("index.SetMapping"), index.SetMapping)
	r.GET("/api/:target/_settings", AuthMiddleware("index.GetSettings"), index.GetSettings)
	r.PUT("/api/:target/_settings", AuthMiddleware("index.SetSettings"), index.SetSettings)
	// analyze
	r.POST("/api/_analyze", AuthMiddleware("index.Analyze"), index.Analyze)
	r.POST("/api/:target/_analyze", AuthMiddleware("index.Analyze"), index.Analyze)

	// search
	r.POST("/api/:target/_search", AuthMiddleware("search.SearchV1"), search.SearchV1)
	r.GET("/api/:target/_count", AuthMiddleware("search.Count"), search.Count)
	r.POST("/api/:target/_count", AuthMiddleware("search.Count"), search.Count)

	// reindex
	r.POST("/api/_reindex", AuthMiddleware("index.Reindex"), index.Reindex)

	// document
	// Document Bulk update/insert
	r.POST("/api/_bulk", AuthMiddleware("document.Bulk"), document.Bulk)
	r.POST("/api/:target/_bulk", AuthMiddleware("document.Bulk"), document.Bulk)
	r.POST("/api/:target/_multi", AuthMiddleware("document.Multi"), document.Multi)
	r.POST("/api/_bulkv2", AuthMiddleware("document.Bulk"), document.Bulkv2)         // New JSON format
	r.POST("/api/:target/_bulkv2", AuthMiddleware("document.Bulk"), document.Bulkv2) // New JSON format
	// Document CRUD APIs. Update is same as create.
	r.POST("/api/:target/_doc", AuthMiddleware("document.Create"), document.CreateUpdate)    // create
	r.PUT("/api/:target/_doc", AuthMiddleware("document.Create"), document.CreateUpdate)     // create
	r.PUT("/api/:target/_doc/:id", AuthMiddleware("document.Create"), document.CreateUpdate) // create or update
	r.HEAD("/api/:target/_doc/:id", AuthMiddleware("document.Get"), document.Get)            // get
	r.GET("/api/:target/_doc/:id", AuthMiddleware("document.Get"), document.Get)             // get
	r.POST("/api/:target/_update/:id", AuthMiddleware("document.Update"), document.Update)   // update
	r.DELETE("/api/:target/_doc/:id", AuthMiddleware("document.Delete"), document.Delete)    // delete

	/**
	 * elastic compatible APIs
	 */

	r.GET("/es/", ESMiddleware, func(c zhttp.Context) {
		zutils.RenderJSON(c, http.StatusOK, elastic.NewESInfo(c))
	})
	r.HEAD("/es/", ESMiddleware, func(c zhttp.Context) {
		zutils.RenderJSON(c, http.StatusOK, elastic.NewESInfo(c))
	})
	r.GET("/es/_license", ESMiddleware, func(c zhttp.Context) {
		zutils.RenderJSON(c, http.StatusOK, elastic.NewESLicense(c))
	})
	r.GET("/es/_xpack", ESMiddleware, func(c zhttp.Context) {
		zutils.RenderJSON(c, http.StatusOK, elastic.NewESXPack(c))
	})

	r.POST("/es/_search", AuthMiddleware("search.SearchDSL"), ESMiddleware, IndexAliasMiddleware, search.SearchDSL)
	r.POST("/es/_msearch", AuthMiddleware("search.MultipleSearch"), ESMiddleware, IndexAliasMiddleware, search.MultipleSearch)
	r.POST("/es/:target/_search", AuthMiddleware("search.SearchDSL"), ESMiddleware, IndexAliasMiddleware, search.SearchDSL)
	r.POST("/es/:target/_msearch", AuthMiddleware("search.MultipleSearch"), ESMiddleware, IndexAliasMiddleware, search.MultipleSearch)
	r.POST("/es/:target/_delete_by_query", AuthMiddleware("search.DeleteByQuery"), IndexAliasMiddleware, search.DeleteByQuery)

	r.GET("/es/_count", AuthMiddleware("search.Count"), ESMiddleware, IndexAliasMiddleware, search.Count)
	r.POST("/es/_count", AuthMiddleware("search.Count"), ESMiddleware, IndexAliasMiddleware, search.Count)
	r.GET("/es/:target/_count", AuthMiddleware("search.Count"), ESMiddleware, IndexAliasMiddleware, search.Count)
	r.POST("/es/:target/_count", AuthMiddleware("search.Count"), ESMiddleware, IndexAliasMiddleware, search.Count)

	r.GET("/es/_field_caps", AuthMiddleware("index.FieldCaps"), ESMiddleware, index.FieldCaps)
	r.POST("/es/_field_caps", AuthMiddleware("index.FieldCaps"), ESMiddleware, index.FieldCaps)
	r.GET("/es/:target/_field_caps", AuthMiddleware("index.FieldCaps"), ESMiddleware, IndexAliasMiddleware, index.FieldCaps)
	r.POST("/es/:target/_field_caps", AuthMiddleware("index.FieldCaps"), ESMiddleware, IndexAliasMiddleware, index.FieldCaps)

	r.POST("/es/_reindex", AuthMiddleware("index.Reindex"), ESMiddleware, index.Reindex)

	// cluster, zedsearch is single node so these are mostly static
	r.GET("/es/_cluster/health", AuthMiddleware("cluster.Health"), ESMiddleware, cluster.Health)
	r.GET("/es/_cluster/health/:target", AuthMiddleware("cluster.Health"), ESMiddleware, cluster.Health)
	r.GET("/es/_cat/health", AuthMiddleware("cluster.CatHealth"), ESMiddleware, cluster.CatHealth)
	r.GET("/es/_cat/indices", AuthMiddleware("cluster.CatIndices"), ESMiddleware, cluster.CatIndices)
	r.GET("/es/_cat/indices/:target", AuthMiddleware("cluster.CatIndices"), ESMiddleware, cluster.CatIndices)

	r.GET("/es/_index_template", AuthMiddleware("index.ListTemplate"), ESMiddleware, index.ListTemplate)
	r.POST("/es/_index_template", AuthMiddleware("index.CreateTemplate"), ESMiddleware, index.CreateTemplate)
	r.PUT("/es/_index_template/:target", AuthMiddleware("index.CreateTemplate"), ESMiddleware, index.CreateTemplate)
	r.GET("/es/_index_template/:target", AuthMiddleware("index.GetTemplate"), ESMiddleware, index.GetTemplate)
	r.HEAD("/es/_index_template/:target", AuthMiddleware("index.GetTemplate"), ESMiddleware, index.GetTemplate)
	r.DELETE("/es/_index_template/:target", AuthMiddleware("index.DeleteTemplate"), ESMiddleware, index.DeleteTemplate)
	// ES Compatible data stream
	r.PUT("/es/_data_stream/:target", AuthMiddleware("elastic.PutDataStream"), ESMiddleware, elastic.PutDataStream)
	r.GET("/es/_data_stream/:target", AuthMiddleware("elastic.GetDataStream"), ESMiddleware, elastic.GetDataStream)
	r.HEAD("/es/_data_stream/:target", AuthMiddleware("elastic.GetDataStream"), ESMiddleware, elastic.GetDataStream)

	r.PUT("/es/:target", AuthMiddleware("index.CreateES"), ESMiddleware, index.CreateES)
	r.HEAD("/es/:target", AuthMiddleware("index.Exists"), ESMiddleware, index.Exists)

	r.GET("/es/:target/_mapping", AuthMiddleware("index.GetESMapping"), ESMiddleware, index.GetESMapping)
	r.PUT("/es/:target/_mapping", AuthMiddleware("index.SetMapping"), ESMiddleware, index.SetMapping)

	r.GET("/es/:target/_settings", AuthMiddleware("index.GetSettings"), ESMiddleware, index.GetSettings)
	r.PUT("/es/:target/_settings", AuthMiddleware("index.SetSettings"), ESMiddleware, index.SetSettings)

	r.POST("/es/_analyze", AuthMiddleware("index.Analyze"), ESMiddleware, index.Analyze)
	r.POST("/es/:target/_analyze", AuthMiddleware("index.Analyze"), ESMiddleware, index.Analyze)

	r.POST("/es/_aliases", AuthMiddleware("index.AddOrRemoveESAlias"), ESMiddleware, index.AddOrRemoveESAlias)

	r.GET("/es/_alias", AuthMiddleware("index.GetESAliases"), ESMiddleware, index.GetESAliases)
	r.GET("/es/:target/_alias", AuthMiddleware("index.GetESAliases"), ESMiddleware, index.GetESAliases)
	r.GET("/es/_alias/:target_alias", AuthMiddleware("index.GetESAliases"), ESMiddleware, index.GetESAliases)

	// ES Bulk update/insert
	r.POST("/es/_bulk", AuthMiddleware("document.ESBulk"), ESMiddleware, document.ESBulk)
	r.POST("/es/:target/_bulk", AuthMiddleware("document.ESBulk"), ESMiddleware, document.ESBulk)
	r.PUT("/es/:target/_bulk", AuthMiddleware("document.ESBulk"), ESMiddleware, document.ESBulk)
	r.POST("/es/:target/_refresh", AuthMiddleware("index.Refresh"), index.Refresh)
	// ES Document
	r.POST("/es/:target/_doc", AuthMiddleware("document.CreateUpdate"), ESMiddleware, document.CreateUpdate)        // create
	r.PUT("/es/:target/_doc/:id", AuthMiddleware("document.CreateUpdate"), ESMiddleware, document.CreateUpdate)     // create or update
	r.PUT("/es/:target/_create/:id", AuthMiddleware("document.CreateUpdate"), ESMiddleware, document.CreateUpdate)  // create
	r.POST("/es/:target/_create/:id", AuthMiddleware("document.CreateUpdate"), ESMiddleware, document.CreateUpdate) // create
	r.POST("/es/:target/_update/:id", AuthMiddleware("document.Update"), ESMiddleware, document.Update)             // update part of document
	r.DELETE("/es/:target/_doc/:id", AuthMiddleware("document.Delete"), ESMiddleware, document.Delete)              // delete
	r.GET("/es/:target/_doc/:id", AuthMiddleware("document.Get"), ESMiddleware, document.Get)                       // get
}
