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
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/zincsearch/zincsearch/pkg/core"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/metadata"
	"github.com/zincsearch/zincsearch/pkg/zutils"
)

// ILMPolicyRequest is the Elasticsearch body of a policy update.
type ILMPolicyRequest struct {
	Policy metadata.ILMPolicy `json:"policy"`
}

// @Id PutILMPolicy
// @Summary Create or update an index lifecycle policy
// @security BasicAuth
// @Tags    Index
// @Accept  json
// @Produce json
// @Param   name   path  string                  true  "Policy"
// @Param   policy body  index.ILMPolicyRequest  true  "Policy"
// @Success 200 {object} meta.HTTPResponse
// @Failure 400 {object} meta.HTTPResponseError
// @Failure 500 {object} meta.HTTPResponseError
// @Router /es/_ilm/policy/{name} [put]
func PutILMPolicy(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: "policy name should be not empty"})
		return
	}
	var req ILMPolicyRequest
	if err := zutils.GinBindJSON(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	if err := checkILMPolicy(req.Policy); err != nil {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	version := int64(1)
	if old, err := metadata.ILM.Get(name); err == nil {
		version = old.Version + 1
	}
	record := metadata.ILMPolicyRecord{
		Name:         name,
		Version:      version,
		ModifiedDate: time.Now().UnixMilli(),
		Policy:       req.Policy,
	}
	if err := metadata.ILM.Set(name, record); err != nil {
		c.JSON(http.StatusInternalServerError, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, meta.HTTPResponse{Message: "ok"})
}

// checkILMPolicy rejects phases zinc cannot act on.
func checkILMPolicy(policy metadata.ILMPolicy) error {
	if len(policy.Phases) == 0 {
		return fmt.Errorf("policy.phases should be not empty")
	}
	for name, phase := range policy.Phases {
		switch name {
		case core.ILMPhaseWarm, core.ILMPhaseCold, core.ILMPhaseDelete, core.TierHot:
		default:
			return fmt.Errorf("policy phase [%s] is invalid, just accept [hot, warm, cold, delete]", name)
		}
		if phase.MinAge == "" {
			continue
		}
		if _, err := core.ParseAge(phase.MinAge); err != nil {
			return err
		}
	}
	return nil
}

// @Id GetILMPolicy
// @Summary Get an index lifecycle policy
// @security BasicAuth
// @Tags    Index
// @Produce json
// @Param   name path string true "Policy"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} meta.HTTPResponseError
// @Router /es/_ilm/policy/{name} [get]
func GetILMPolicy(c *gin.Context) {
	name := c.Param("name")
	record, err := metadata.ILM.Get(name)
	if err != nil {
		c.JSON(http.StatusNotFound, meta.HTTPResponseError{Error: "ilm policy " + name + " does not exists"})
		return
	}
	c.JSON(http.StatusOK, gin.H{name: policyResponse(record)})
}

// @Id ListILMPolicy
// @Summary List index lifecycle policies
// @security BasicAuth
// @Tags    Index
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} meta.HTTPResponseError
// @Router /es/_ilm/policy [get]
func ListILMPolicy(c *gin.Context) {
	records, err := metadata.ILM.List(0, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	out := gin.H{}
	for _, record := range records {
		out[record.Name] = policyResponse(record)
	}
	c.JSON(http.StatusOK, out)
}

// @Id DeleteILMPolicy
// @Summary Delete an index lifecycle policy
// @security BasicAuth
// @Tags    Index
// @Produce json
// @Param   name path string true "Policy"
// @Success 200 {object} meta.HTTPResponse
// @Failure 500 {object} meta.HTTPResponseError
// @Router /es/_ilm/policy/{name} [delete]
func DeleteILMPolicy(c *gin.Context) {
	name := c.Param("name")
	if err := metadata.ILM.Delete(name); err != nil {
		c.JSON(http.StatusInternalServerError, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, meta.HTTPResponse{Message: "ok"})
}

func policyResponse(record *metadata.ILMPolicyRecord) gin.H {
	return gin.H{
		"version":       record.Version,
		"modified_date": record.ModifiedDate,
		"policy":        record.Policy,
	}
}

// @Id ExplainILM
// @Summary Explain the lifecycle state of an index
// @security BasicAuth
// @Tags    Index
// @Produce json
// @Param   index path string true "Index"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} meta.HTTPResponseError
// @Router /es/{index}/_ilm/explain [get]
func ExplainILM(c *gin.Context) {
	indexName := c.Param("target")
	index, exists := core.GetIndex(indexName)
	if !exists {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: "index " + indexName + " does not exists"})
		return
	}

	explain := gin.H{
		"index":        index.GetName(),
		"tier":         index.GetTier(),
		"storage_type": index.GetStorageType(),
		"read_only":    index.IsReadOnly(),
		"managed":      false,
	}
	lifecycle, err := core.ResolveLifecycle(index)
	if err != nil {
		c.JSON(http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	if lifecycle != nil {
		explain["managed"] = true
		explain["policy"] = lifecycle.Policy
		explain["lifecycle"] = lifecycle
	}
	age, action, next := core.ExplainTiering(index, lifecycle, time.Now())
	explain["age"] = age.String()
	explain["age_millis"] = age.Milliseconds()
	explain["action"] = action
	explain["next_tier"] = next

	c.JSON(http.StatusOK, gin.H{"indices": gin.H{index.GetName(): explain}})
}
