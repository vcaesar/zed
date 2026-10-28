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
	"github.com/vcaesar/zedsearch/pkg/handlers/sql"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
)

// SetSQLRoutes sets up the SQL endpoints, native and Elasticsearch compatible.
func SetSQLRoutes(r *zhttp.Engine) {
	r.POST("/api/_sql", AuthMiddleware("sql.Search"), sql.Search)
	r.POST("/es/_sql", AuthMiddleware("sql.Search"), ESMiddleware, sql.Search)
	r.POST("/es/_sql/translate", AuthMiddleware("sql.Translate"), ESMiddleware, sql.Translate)
}
