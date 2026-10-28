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

package search

import (
	"net/http"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta"
	v1 "github.com/vcaesar/zedsearch/pkg/meta/v1"
	"github.com/vcaesar/zedsearch/pkg/uquery"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

// SearchV1 searches the index for the given http request from end user
//
// @Id SearchV1
// @Summary Search V1
// @security BasicAuth
// @Tags    Search
// @Accept  json
// @Produce json
// @Param   index  path  string  true  "Index"
// @Param   query  body  v1.ZedQueryForSDK  true  "Query"
// @Success 200 {object} meta.SearchResponse
// @Failure 400 {object} meta.HTTPResponseError
// @Router /api/{index}/_search [post]
func SearchV1(c zhttp.Context) {
	indexName := c.Param("target")
	index, exists := core.GetIndex(indexName)
	if !exists {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: "index " + indexName + " does not exists"})
		return
	}

	iQuery := new(v1.ZedQuery)
	iQuery.MaxResults = 10
	if err := zutils.BindJSON(c, &iQuery); err != nil {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	// convert to new query DSL
	newQuery, err := uquery.ParseQueryDSLFromV1(iQuery)
	if err != nil {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	resp, err := index.Search(newQuery)
	if err != nil {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	storageSize := index.GetStats().StorageSize
	eventData := make(map[string]interface{})
	eventData["search_type"] = iQuery.SearchType
	eventData["search_index_storage"] = index.GetStorageType()
	eventData["search_index_size_in_mb"] = storageSize / 1024 / 1024
	eventData["time_taken_to_search_in_ms"] = resp.Took
	eventData["aggregations_count"] = len(iQuery.Aggregations)
	core.Telemetry.Event("search", eventData)

	c.JSON(http.StatusOK, resp)
}
