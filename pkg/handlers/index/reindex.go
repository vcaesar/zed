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

package index

import (
	"bytes"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/zincsearch/zincsearch/pkg/core"
	"github.com/zincsearch/zincsearch/pkg/errors"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/meta/elastic"
	"github.com/zincsearch/zincsearch/pkg/zutils"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

// reindexBatchSize is the number of documents copied per search request.
const reindexBatchSize = 1000

// ReindexRequest is the request body of the _reindex API.
type ReindexRequest struct {
	Source ReindexSource `json:"source"`
	Dest   ReindexDest   `json:"dest"`
	// MaxDocs limits the number of copied documents, 0 means all of them.
	MaxDocs int `json:"max_docs"`
}

type ReindexSource struct {
	Index string      `json:"index"`
	Query interface{} `json:"query"`
}

type ReindexDest struct {
	Index string `json:"index"`
}

// Reindex copies the documents of one index into another one
//
// @Id Reindex
// @Summary Reindex documents for compatible ES
// @security BasicAuth
// @Tags    Index
// @Accept  json
// @Produce json
// @Param   query  body  ReindexRequest true  "Reindex"
// @Success 200 {object} elastic.ReindexResponse
// @Failure 400 {object} meta.HTTPResponseError
// @Router /es/_reindex [post]
func Reindex(c *gin.Context) {
	start := time.Now()

	req := new(ReindexRequest)
	body, err := readOptionalBody(c)
	if err != nil {
		log.Printf("handlers.index.Reindex: %s", err.Error())
		zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, req); err != nil {
			log.Printf("handlers.index.Reindex: %s", err.Error())
			zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
			return
		}
	}

	if req.Source.Index == "" || req.Dest.Index == "" {
		zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: "source.index and dest.index are required"})
		return
	}
	if req.Source.Index == req.Dest.Index {
		zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: "source.index and dest.index must be different"})
		return
	}

	source, exists := core.GetIndex(req.Source.Index)
	if !exists {
		zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: "index " + req.Source.Index + " does not exists"})
		return
	}
	dest, _, err := core.GetOrCreateIndex(req.Dest.Index, "", 0)
	if err != nil {
		zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	total := 0
	created := 0
	batches := 0
	failures := []string{}
	for {
		size := reindexBatchSize
		if req.MaxDocs > 0 && req.MaxDocs-total < size {
			size = req.MaxDocs - total
		}
		if size <= 0 {
			break
		}

		// sort by _id so paging with from/size stays stable between batches
		resp, err := source.Search(&meta.ZincQuery{
			Query: req.Source.Query,
			Sort:  []interface{}{"_id"},
			From:  total,
			Size:  size,
		})
		if err != nil {
			errors.HandleError(c, err)
			return
		}
		if len(resp.Hits.Hits) == 0 {
			break
		}
		batches++

		for _, hit := range resp.Hits.Hits {
			doc, ok := hit.Source.(map[string]interface{})
			if !ok {
				failures = append(failures, hit.ID)
				continue
			}
			// _source carries @timestamp as time.Time, the write path wants a parsable value
			doc[meta.TimeFieldName] = hit.Timestamp.Format(time.RFC3339Nano)
			if err := dest.CreateDocument(hit.ID, doc, true); err != nil {
				log.Error().Err(err).Msgf("handlers.index.Reindex: index [%s] doc [%s]", dest.GetName(), hit.ID)
				failures = append(failures, hit.ID)
				continue
			}
			created++
		}

		total += len(resp.Hits.Hits)
		if len(resp.Hits.Hits) < size {
			break
		}
	}

	zutils.GinRenderJSON(c, http.StatusOK, elastic.ReindexResponse{
		Took:                 time.Since(start).Milliseconds(),
		TimedOut:             false,
		Total:                total,
		Created:              created,
		Updated:              0,
		Deleted:              0,
		Batches:              batches,
		VersionConflicts:     0,
		Noops:                0,
		Retries:              meta.HttpRetriesResponse{Bulk: 0, Search: 0},
		ThrottledMillis:      0,
		RequestsPerSecond:    -1,
		ThrottledUntilMillis: 0,
		Failures:             failures,
	})
}
