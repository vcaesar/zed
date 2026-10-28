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
	"bytes"
	"io"
	"net/http"
	"sort"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/meta/elastic"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
	"github.com/vcaesar/zedsearch/pkg/zutils"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// FieldCapsRequest is the optional request body of the _field_caps API.
type FieldCapsRequest struct {
	Fields []string `json:"fields"`
}

// FieldCaps returns the capabilities of the fields of the given indexes
//
// @Id FieldCaps
// @Summary Get field capabilities for compatible ES
// @security BasicAuth
// @Tags    Index
// @Accept  json
// @Produce json
// @Param   index  path   string  true   "Index"
// @Param   fields query  string  false  "Field patterns, comma separated"
// @Success 200 {object} elastic.FieldCapsResponse
// @Failure 400 {object} meta.HTTPResponseError
// @Router /es/{index}/_field_caps [post]
func FieldCaps(c zhttp.Context) {
	target := c.Param("target")

	patterns := elastic.SplitPatterns(c.Query("fields"))

	// the body is optional, it may carry additional field patterns
	body, err := readOptionalBody(c)
	if err != nil {
		log.Printf("handlers.index.FieldCaps: %s", err.Error())
		zutils.RenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	if len(bytes.TrimSpace(body)) > 0 {
		req := new(FieldCapsRequest)
		if err := json.Unmarshal(body, req); err != nil {
			log.Printf("handlers.index.FieldCaps: %s", err.Error())
			zutils.RenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
			return
		}
		patterns = append(patterns, req.Fields...)
	}

	indexPatterns := elastic.SplitPatterns(target)
	indexes := make([]*core.Index, 0, 1)
	for _, index := range core.ZED_INDEX_LIST.List() {
		if elastic.MatchAnyPattern(index.GetName(), indexPatterns) {
			indexes = append(indexes, index)
		}
	}
	if len(indexes) == 0 && target != "" {
		zutils.RenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: "index " + target + " does not exists"})
		return
	}

	resp := elastic.FieldCapsResponse{
		Indices: make([]string, 0, len(indexes)),
		Fields:  make(map[string]map[string]elastic.FieldCap),
	}
	for _, index := range indexes {
		resp.Indices = append(resp.Indices, index.GetName())
		mappings := index.GetMappings()
		if mappings == nil {
			continue
		}
		for field, prop := range mappings.ListProperty() {
			if !elastic.MatchAnyPattern(field, patterns) {
				continue
			}
			typ := esFieldType(prop.Type)
			caps, ok := resp.Fields[field]
			if !ok {
				caps = make(map[string]elastic.FieldCap)
				resp.Fields[field] = caps
			}
			caps[typ] = elastic.FieldCap{
				Type:          typ,
				MetadataField: false,
				Searchable:    prop.Index,
				Aggregatable:  prop.Aggregatable,
			}
		}
	}
	sort.Strings(resp.Indices)

	zutils.RenderJSON(c, http.StatusOK, resp)
}

// readOptionalBody reads the request body, GET requests may not carry one at all.
func readOptionalBody(c zhttp.Context) ([]byte, error) {
	return io.ReadAll(c.Body())
}

// esFieldType maps a zed property type to the elasticsearch field type.
func esFieldType(typ string) string {
	switch typ {
	case "numeric":
		return "double"
	case "vector":
		return "dense_vector"
	case "bool":
		return "boolean"
	default:
		return typ
	}
}
