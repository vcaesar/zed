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

// Package sql exposes the Elasticsearch SQL endpoints.
package sql

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	zerrors "github.com/vcaesar/zedsearch/pkg/errors"
	"github.com/vcaesar/zedsearch/pkg/meta"
	zincsql "github.com/vcaesar/zedsearch/pkg/sql"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

// Request is the body of a SQL request.
type Request struct {
	Query     string `json:"query"`
	FetchSize int    `json:"fetch_size"`
	Format    string `json:"format"`
}

// Search runs a SQL query and renders the rows it produced
//
// @Id SQLSearch
// @Summary SQL search for compatible ES
// @security BasicAuth
// @Tags    Search
// @Accept  json
// @Produce json
// @Param   query  body  sql.Request  true  "Query"
// @Param   format query string false "Response format: json, txt, csv, tsv"
// @Success 200 {object} sql.Response
// @Failure 400 {object} meta.HTTPResponseError
// @Router /es/_sql [post]
func Search(c *gin.Context) {
	req, ok := bindRequest(c)
	if !ok {
		return
	}

	format, err := zincsql.ParseFormat(requestFormat(c, req), c.GetHeader("Accept"))
	if err != nil {
		renderError(c, err)
		return
	}

	resp, err := zincsql.Query(req.Query, req.FetchSize)
	if err != nil {
		renderError(c, err)
		return
	}

	data, err := resp.Render(format)
	if err != nil {
		renderError(c, err)
		return
	}
	c.Data(http.StatusOK, zincsql.ContentType(format), data)
}

// Translate returns the query DSL a SQL query maps to
//
// @Id SQLTranslate
// @Summary SQL translate to query DSL for compatible ES
// @security BasicAuth
// @Tags    Search
// @Accept  json
// @Produce json
// @Param   query  body  sql.Request  true  "Query"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} meta.HTTPResponseError
// @Router /es/_sql/translate [post]
func Translate(c *gin.Context) {
	req, ok := bindRequest(c)
	if !ok {
		return
	}

	dsl, err := zincsql.Translate(req.Query, req.FetchSize)
	if err != nil {
		renderError(c, err)
		return
	}
	zutils.GinRenderJSON(c, http.StatusOK, dsl)
}

func bindRequest(c *gin.Context) (*Request, bool) {
	req := new(Request)
	if err := zutils.GinBindJSON(c, req); err != nil {
		log.Printf("handlers.sql.bindRequest: %s", err.Error())
		zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return nil, false
	}
	if req.Query == "" {
		zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: "query is required"})
		return nil, false
	}
	return req, true
}

// requestFormat prefers the format of the body, then the query string.
func requestFormat(c *gin.Context, req *Request) string {
	if req.Format != "" {
		return req.Format
	}
	return c.Query("format")
}

func renderError(c *gin.Context, err error) {
	var sqlErr *zincsql.Error
	if zerrors.As(err, &sqlErr) {
		log.Printf("handlers.sql: %s", err.Error())
		zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	zerrors.HandleError(c, err)
}
