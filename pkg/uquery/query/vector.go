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

package query

import (
	"fmt"
	"strings"

	"github.com/vcaesar/riot"

	"github.com/zincsearch/zincsearch/pkg/errors"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils"
)

const knnDefaultK = 10

// knnMetrics maps Elasticsearch/OpenSearch similarity names to riot metrics.
var knnMetrics = map[string]riot.Metric{
	"cosine": riot.Cosine, "cosinesimil": riot.Cosine,
	"dot_product": riot.DotProduct, "dot": riot.DotProduct, "innerproduct": riot.DotProduct,
	"l2_norm": riot.L2, "l2": riot.L2, "euclidean": riot.L2,
}

// KnnQuery parses an exact k-nearest-neighbours query over a vector field.
// As in Elasticsearch, k is the number of neighbours per shard; use size to
// cap the merged hits.
// Elasticsearch form:
//
//	{"field": "embedding", "query_vector": [0.1, 0.2], "k": 5, "similarity": "cosine", "boost": 2}
//
// OpenSearch form:
//
//	{"embedding": {"vector": [0.1, 0.2], "k": 5}}
func KnnQuery(query map[string]interface{}, mappings *meta.Mappings) (riot.Query, error) {
	var field string
	var vector interface{}
	k, boost := knnDefaultK, -1.0
	metric := riot.Cosine
	var err error
	for key, v := range query {
		switch strings.ToLower(key) {
		case "field":
			if field, err = zutils.ToString(v); err != nil {
				return nil, errors.New(errors.ErrorTypeParsingException, "[knn] field must be a string")
			}
		case "query_vector", "vector":
			vector = v
		case "k":
			if k, err = zutils.ToInt(v); err != nil || k <= 0 {
				return nil, errors.New(errors.ErrorTypeParsingException, "[knn] k must be a positive integer")
			}
		case "similarity", "metric", "space_type":
			name, err := zutils.ToString(v)
			if err != nil {
				return nil, errors.New(errors.ErrorTypeParsingException, "[knn] similarity must be a string")
			}
			m, ok := knnMetrics[strings.ToLower(name)]
			if !ok {
				return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[knn] unsupported similarity %q, expected cosine, dot_product or l2_norm", name))
			}
			metric = m
		case "boost":
			if boost, err = zutils.ToFloat64(v); err != nil {
				return nil, errors.New(errors.ErrorTypeParsingException, "[knn] boost must be numeric")
			}
		case "num_candidates", "filter", "_name":
			// exact search: candidates are all documents; filter composes via bool
		default:
			opts, ok := v.(map[string]interface{})
			if !ok || field != "" || len(query) != 1 {
				return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[knn] unknown option [%s]", key))
			}
			if _, ok := opts["field"]; ok {
				return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[knn] field [%s] doesn't support option [field]", key))
			}
			flat := make(map[string]interface{}, len(opts)+1)
			for k, v := range opts {
				flat[k] = v
			}
			flat["field"] = key
			return KnnQuery(flat, mappings)
		}
	}
	if field == "" {
		return nil, errors.New(errors.ErrorTypeParsingException, "[knn] requires field")
	}
	prop, _ := mappings.GetProperty(field)
	if prop.Type != "vector" {
		return nil, errors.New(errors.ErrorTypeXContentParseException,
			fmt.Sprintf("[knn] field [%s] is not of type [vector], got %q", field, prop.Type))
	}
	if vector == nil {
		return nil, errors.New(errors.ErrorTypeParsingException, "[knn] requires query_vector")
	}
	vec, err := meta.ParseVector(vector, prop.Dims)
	if err != nil {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[knn] query_vector %s", err))
	}
	subq := riot.NewKNNQuery(field, vec, k).SetMetric(metric)
	if err := subq.Validate(); err != nil {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[knn] %s", err))
	}
	if boost >= 0 {
		subq.SetBoost(boost)
	}
	return subq, nil
}
