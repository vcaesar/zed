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

package search

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/zincsearch/zincsearch/pkg/core"
	"github.com/zincsearch/zincsearch/pkg/errors"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/meta/elastic"
	"github.com/zincsearch/zincsearch/pkg/zutils"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

// Count returns the number of documents matching a query
//
// @Id Count
// @Summary Count documents for compatible ES
// @security BasicAuth
// @Tags    Search
// @Accept  json
// @Produce json
// @Param   index  path  string  true   "Index"
// @Param   query  body  meta.ZincQueryForSDK false  "Query"
// @Success 200 {object} elastic.CountResponse
// @Failure 400 {object} meta.HTTPResponseError
// @Router /es/{index}/_count [post]
func Count(c *gin.Context) {
	indexName := c.Param("target")

	// the body is optional, an empty body counts all documents
	body, err := readOptionalBody(c)
	if err != nil {
		log.Printf("handlers.search.Count: %s", err.Error())
		zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	query := &meta.ZincQuery{}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, query); err != nil {
			log.Printf("handlers.search.Count: %s", err.Error())
			zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
			return
		}
	}

	// count runs the regular search path, it just doesn't need any hit back
	query.From = 0
	query.Size = 0
	query.TrackTotalHits = true
	query.Aggregations = nil
	query.Highlight = nil
	query.Sort = nil

	indexNames := make([]string, 0, 1)
	if indexName != "" {
		indexNames = strings.Split(indexName, ",")
	}

	resp, err := searchIndex(indexNames, query)
	if err != nil {
		errors.HandleError(c, err)
		return
	}

	shards := countShards(indexNames)
	zutils.GinRenderJSON(c, http.StatusOK, elastic.CountResponse{
		Count: int64(resp.Hits.Total.Value),
		Shards: meta.Shards{
			Total:      shards,
			Successful: shards,
			Skipped:    0,
			Failed:     0,
		},
	})
}

// readOptionalBody reads the request body, GET requests may not carry one at all.
func readOptionalBody(c *gin.Context) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}
	defer c.Request.Body.Close()
	return io.ReadAll(c.Request.Body)
}

// countShards sums the shards of every index matching indexNames, it never returns less than 1.
func countShards(indexNames []string) int64 {
	var shards int64
	for _, index := range core.ZINC_INDEX_LIST.List() {
		if !elastic.MatchAnyPattern(index.GetName(), indexNames) {
			continue
		}
		shards += index.GetShardNum()
	}
	if shards <= 0 {
		shards = 1
	}
	return shards
}
