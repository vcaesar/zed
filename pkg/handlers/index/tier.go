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

package index

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

// TierRequest is the body of a tier change.
type TierRequest struct {
	Tier string `json:"tier"`
}

// @Id GetIndexTier
// @Summary Get index data tier
// @security BasicAuth
// @Tags    Index
// @Produce json
// @Param   index path  string  true  "Index"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} meta.HTTPResponseError
// @Router /api/index/{index}/_tier [get]
func GetTier(c *gin.Context) {
	indexName := c.Param("target")
	index, exists := core.GetIndex(indexName)
	if !exists {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: "index " + indexName + " does not exists"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"index":        index.GetName(),
		"tier":         index.GetTier(),
		"storage_type": index.GetStorageType(),
		"read_only":    index.IsReadOnly(),
	})
}

// @Id SetIndexTier
// @Summary Move index to another data tier
// @security BasicAuth
// @Tags    Index
// @Accept  json
// @Produce json
// @Param   index path  string           true  "Index"
// @Param   tier  body  index.TierRequest true "Tier"
// @Success 200 {object} meta.HTTPResponse
// @Failure 400 {object} meta.HTTPResponseError
// @Failure 500 {object} meta.HTTPResponseError
// @Router /api/index/{index}/_tier [put]
func SetTier(c *gin.Context) {
	indexName := c.Param("target")
	var req TierRequest
	if err := zutils.GinBindJSON(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	if err := core.CheckTier(req.Tier); err != nil {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	if _, exists := core.GetIndex(indexName); !exists {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: "index " + indexName + " does not exists"})
		return
	}
	if err := core.MoveIndexTier(indexName, req.Tier); err != nil {
		c.JSON(http.StatusInternalServerError, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok", "index": indexName, "tier": req.Tier})
}
