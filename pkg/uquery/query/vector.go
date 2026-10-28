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

package query

import (
	"fmt"
	"math"
	"strings"

	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis"

	"github.com/vcaesar/zedsearch/pkg/errors"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

const (
	knnDefaultK         = 10
	knnMaxNumCandidates = 10000
)

// KnnQuery parses a k-nearest-neighbours query over a vector field.
// As in Elasticsearch, k is the number of neighbours per shard; use size to
// cap the merged hits. The metric is the mapping similarity (space_type),
// cosine by default. The search is HNSW approximate when the vector mapping
// declares index_options (or OpenSearch method), or when num_candidates (or
// OpenSearch method_parameters.ef_search) is set as the search beam; otherwise
// it is exact. Graphs are built per segment on first use and persisted next
// to it, so later searches and restarts reuse them. filter restricts the
// candidates before ranking and a numeric similarity drops matches below it.
// Elasticsearch form:
//
//	{"field": "embedding", "query_vector": [0.1, 0.2], "k": 5, "num_candidates": 50, "filter": {...}, "similarity": 0.8, "boost": 2}
//
// OpenSearch form:
//
//	{"embedding": {"vector": [0.1, 0.2], "k": 5, "filter": {...}, "method_parameters": {"ef_search": 50}}}
func KnnQuery(query map[string]interface{}, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) (riot.Query, error) {
	var field string
	var vector, filter interface{}
	var metric riot.Metric
	var similarity *float64
	k, boost := knnDefaultK, -1.0
	numCandidates := 0
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
			// a number is the Elasticsearch minimum similarity, a name overrides the mapping metric
			if name, ok := v.(string); ok {
				if metric, err = meta.ParseVectorMetric(name); err != nil {
					return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[knn] %s", err))
				}
				continue
			}
			min, ok := v.(float64)
			if !ok || math.IsNaN(min) || math.IsInf(min, 0) {
				return nil, errors.New(errors.ErrorTypeParsingException, "[knn] similarity must be a number or a metric name")
			}
			similarity = &min
		case "boost":
			if boost, err = zutils.ToFloat64(v); err != nil {
				return nil, errors.New(errors.ErrorTypeParsingException, "[knn] boost must be numeric")
			}
		case "num_candidates":
			if numCandidates, err = zutils.ToInt(v); err != nil || numCandidates <= 0 {
				return nil, errors.New(errors.ErrorTypeParsingException, "[knn] num_candidates must be a positive integer")
			}
		case "method_parameters":
			params, ok := v.(map[string]interface{})
			if !ok {
				return nil, errors.New(errors.ErrorTypeParsingException, "[knn] method_parameters must be an object")
			}
			for name, pv := range params {
				if strings.ToLower(name) != "ef_search" {
					return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[knn] unknown method parameter [%s]", name))
				}
				if numCandidates, err = zutils.ToInt(pv); err != nil || numCandidates <= 0 {
					return nil, errors.New(errors.ErrorTypeParsingException, "[knn] ef_search must be a positive integer")
				}
			}
		case "filter":
			filter = v
		case "_name":
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
			return KnnQuery(flat, mappings, analyzers)
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
	if k > knnMaxNumCandidates {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[knn] k cannot exceed %d", knnMaxNumCandidates))
	}
	if metric == "" {
		metric = prop.Similarity
	}
	if metric == "" {
		metric = riot.Cosine
	}
	subq := riot.NewKNNQuery(field, vec, k).SetMetric(metric)
	var ann *riot.ANNParams
	if opts := prop.IndexOptions; opts != nil {
		// graphs are persisted per segment, keyed by these build parameters
		ann = &riot.ANNParams{M: opts.M, EfConstruction: opts.EfConstruction}
	}
	if numCandidates > 0 {
		if numCandidates < k {
			return nil, errors.New(errors.ErrorTypeParsingException, "[knn] num_candidates cannot be less than k")
		}
		if numCandidates > knnMaxNumCandidates {
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[knn] num_candidates cannot exceed %d", knnMaxNumCandidates))
		}
		if ann == nil {
			ann = new(riot.ANNParams)
		}
		ann.EfSearch = numCandidates
	}
	if ann != nil {
		subq.SetANN(*ann)
	}
	if err := subq.Validate(); err != nil {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[knn] %s", err))
	}
	if boost >= 0 {
		subq.SetBoost(boost)
	}
	if filter == nil && similarity == nil {
		return subq, nil
	}
	q := &knnQuery{knn: subq, similarity: similarity}
	if filter != nil {
		if q.filter, err = knnFilter(filter, mappings, analyzers); err != nil {
			return nil, err
		}
	}
	return q, nil
}

// knnFilter parses the filter of a knn query: one query or an array of
// queries that must all match.
func knnFilter(v interface{}, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) (riot.Query, error) {
	parse := func(v interface{}) (riot.Query, error) {
		q, err := Query(v, mappings, analyzers)
		if err != nil {
			return nil, errors.New(errors.ErrorTypeXContentParseException, "[knn] failed to parse filter").Cause(err)
		}
		return q, nil
	}
	list, ok := v.([]interface{})
	if !ok {
		return parse(v)
	}
	bq := riot.NewBooleanQuery()
	for _, f := range list {
		q, err := parse(f)
		if err != nil {
			return nil, err
		}
		bq.AddMust(q)
	}
	return bq, nil
}
