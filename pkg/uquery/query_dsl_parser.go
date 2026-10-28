/* Copyright 2022 Zinc Labs Inc. and Contributors
* Copyright 2026 AtomAI
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

package uquery

import (
	"fmt"

	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/search"

	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/errors"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/uquery/aggregation"
	"github.com/vcaesar/zedsearch/pkg/uquery/fields"
	"github.com/vcaesar/zedsearch/pkg/uquery/highlight"
	"github.com/vcaesar/zedsearch/pkg/uquery/query"
	"github.com/vcaesar/zedsearch/pkg/uquery/sort"
	"github.com/vcaesar/zedsearch/pkg/uquery/source"
)

// ParseQueryDSL parse query DSL and return searchRequest
func ParseQueryDSL(q *meta.ZedQuery, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) (riot.SearchRequest, error) {
	// parse size
	if q.Size > config.Global.MaxResults {
		q.Size = config.Global.MaxResults
	}

	// parse query
	query, err := parseQuery(q, mappings, analyzers)
	if err != nil {
		return nil, err
	}
	if query == nil {
		return nil, errors.New(errors.ErrorTypeNotImplemented, fmt.Sprintf("[%s] query doesn't support", q.Query))
	}

	// create search request
	request := riot.NewTopNSearch(q.Size, query).WithStandardAggregations()

	// parse highlight
	if q.Highlight != nil {
		_ = highlight.Request(q.Highlight)
		request.IncludeLocations()
	}

	// parse from
	if q.From > 0 {
		request.SetFrom(q.From)
	}

	// parse explain
	if q.Explain {
		request.ExplainScores()
	}

	// parse aggregations
	if q.Aggregations != nil {
		if err := aggregation.Request(request, q.Aggregations, mappings); err != nil {
			return nil, err
		}
	}

	// parse fields
	if q.Fields != nil {
		if v, ok := q.Fields.([]interface{}); ok {
			if q.Fields, err = fields.Request(v); err != nil {
				return nil, err
			}
		}
	}

	// parse source
	if q.Source, err = source.Request(q.Source); err != nil {
		return nil, err
	}

	// parse sort
	if q.Sort != nil {
		if q.Sort, err = sort.Request(q.Sort); err != nil {
			return nil, err
		}
		if q.Sort != nil {
			request.SortByCustom(q.Sort.(search.SortOrder))
		}
	}

	// pagenation
	// TODO: search after PIT support

	return request, nil
}

// parseQuery parses the query and the Elasticsearch top-level knn, one search
// or an array of them. Hits match the query or any knn and their scores add
// up; without a query only knn hits are returned.
func parseQuery(q *meta.ZedQuery, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) (riot.Query, error) {
	if q.Knn == nil {
		return query.Query(q.Query, mappings, analyzers)
	}
	knns, ok := q.Knn.([]interface{})
	if !ok {
		knns = []interface{}{q.Knn}
	}
	bq := riot.NewBooleanQuery()
	if q.Query != nil {
		subq, err := query.Query(q.Query, mappings, analyzers)
		if err != nil {
			return nil, err
		}
		bq.AddShould(subq)
	}
	for _, v := range knns {
		knn, ok := v.(map[string]interface{})
		if !ok {
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[knn] must be an object or an array of objects, got %T", v))
		}
		subq, err := query.KnnQuery(knn, mappings, analyzers)
		if err != nil {
			return nil, errors.New(errors.ErrorTypeXContentParseException, "[knn] failed to parse field").Cause(err)
		}
		bq.AddShould(subq)
	}
	return bq, nil
}
