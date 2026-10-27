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

package cluster

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/zincsearch/zincsearch/pkg/cluster"
	"github.com/zincsearch/zincsearch/pkg/core"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils"
)

// InternalSearchRequest is what a coordinating node sends to every owner
// during a scatter-gather search.
type InternalSearchRequest struct {
	Indexes []string        `json:"indexes"`
	Query   *meta.ZincQuery `json:"query"`
}

// IndexOwner is one entry of GET /api/_internal/indexes.
type IndexOwner struct {
	Name        string `json:"name"`
	StorageType string `json:"storage_type"`
	Partition   int    `json:"partition"`
	Owner       string `json:"owner,omitempty"`
	Local       bool   `json:"local"`
}

// InternalIndexesResponse is the payload of GET /api/_internal/indexes.
type InternalIndexesResponse struct {
	Node    string       `json:"node"`
	Indexes []IndexOwner `json:"indexes"`
}

// InternalSearch runs a search over the indexes this node owns
//
// It is the node-to-node half of a scatter-gather search: the coordinating
// node splits the requested indexes by owner and merges the responses. The
// caller's Authorization header is forwarded, so the same permissions apply.
//
// @Id InternalSearch
// @Summary Search indexes owned by this node
// @security BasicAuth
// @Tags Cluster
// @Accept json
// @Produce json
// @Param query body InternalSearchRequest true "Query"
// @Success 200 {object} meta.SearchResponse
// @Failure 400 {object} meta.HTTPResponseError
// @Router /api/_internal/search [post]
func InternalSearch(c *gin.Context) {
	req := new(InternalSearchRequest)
	if err := zutils.GinBindJSON(c, req); err != nil {
		zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	if req.Query == nil {
		req.Query = &meta.ZincQuery{Size: 10}
	}

	resp, err := searchLocal(req.Indexes, req.Query)
	if err != nil {
		log.Error().Err(err).Strs("indexes", req.Indexes).Msg("cluster: internal search")
		zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	addPartialAggregations(resp, req.Query)

	zutils.GinRenderJSON(c, http.StatusOK, resp)
}

// InternalIndexes lists the indexes known to this node
//
// Used to resolve index patterns and to debug ownership.
//
// @Id InternalIndexes
// @Summary List indexes known to this node
// @security BasicAuth
// @Tags Cluster
// @Produce json
// @Success 200 {object} InternalIndexesResponse
// @Router /api/_internal/indexes [get]
func InternalIndexes(c *gin.Context) {
	out := InternalIndexesResponse{}
	if node := cluster.Local(); node != nil {
		out.Node = node.ID
	}

	for _, index := range core.ZINC_INDEX_LIST.List() {
		name := index.GetName()
		owner, _, _ := cluster.Owner(name)
		out.Indexes = append(out.Indexes, IndexOwner{
			Name:        name,
			StorageType: index.GetStorageType(),
			Partition:   cluster.PartitionOf(name),
			Owner:       owner,
			Local:       cluster.IsLocalOwner(name),
		})
	}
	sort.Slice(out.Indexes, func(i, j int) bool { return out.Indexes[i].Name < out.Indexes[j].Name })

	zutils.GinRenderJSON(c, http.StatusOK, out)
}

// searchLocal runs the query against local indexes, the same way the user
// facing search handler does.
func searchLocal(indexes []string, query *meta.ZincQuery) (*meta.SearchResponse, error) {
	if len(indexes) == 1 && !strings.Contains(indexes[0], "*") {
		index, ok := core.GetIndex(indexes[0])
		if !ok {
			return nil, fmt.Errorf("index %s does not exists", indexes[0])
		}
		return index.Search(query)
	}
	return core.MultiSearch(indexes, query)
}

// addPartialAggregations attaches the {count, sum} counters an average
// needs to be merged correctly on the coordinator. Without them the
// coordinator can only take an unweighted mean of the per-node averages.
//
// The count is the number of documents this node matched, which is exact
// as long as the averaged field is present in every matched document.
func addPartialAggregations(resp *meta.SearchResponse, query *meta.ZincQuery) {
	if resp == nil || len(resp.Aggregations) == 0 {
		return
	}
	count := uint64(resp.Hits.Total.Value)
	for name, def := range query.Aggregations {
		if def.Avg == nil {
			continue
		}
		agg, ok := resp.Aggregations[name]
		if !ok {
			continue
		}
		average, ok := agg.Value.(float64)
		if !ok {
			continue
		}
		sum := average * float64(count)
		partialCount := count
		if agg.Aggregations == nil {
			agg.Aggregations = make(map[string]meta.AggregationResponse, 1)
		}
		agg.Aggregations[cluster.PartialAggName] = meta.AggregationResponse{
			Count: &partialCount,
			Sum:   &sum,
		}
		resp.Aggregations[name] = agg
	}
}
