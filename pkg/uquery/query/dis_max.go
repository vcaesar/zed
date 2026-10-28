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
	"strings"

	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis"

	"github.com/vcaesar/zedsearch/pkg/errors"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

// DisMaxQuery returns documents matching at least one of the given queries.
//
// riot has no disjunction-max searcher, so this is approximated with a boolean
// should query with minimum_should_match=1. Matching documents are therefore
// scored with the SUM of the matching clause scores instead of the maximum
// one; `tie_breaker` is accepted for API compatibility but is not applied to
// scoring. The matched document set is identical to Elasticsearch's dis_max,
// only the relative ordering of multi-clause matches can differ.
func DisMaxQuery(query map[string]interface{}, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) (riot.Query, error) {
	disMaxQuery := riot.NewBooleanQuery()
	queries := 0
	for k, v := range query {
		k := strings.ToLower(k)
		switch k {
		case "queries":
			vv, ok := v.([]interface{})
			if !ok {
				return nil, errors.New(errors.ErrorTypeParsingException, "[dis_max] queries must be a non-empty array")
			}
			for _, item := range vv {
				sub, ok := item.(map[string]interface{})
				if !ok {
					return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[dis_max] queries doesn't support values of type: %T", item))
				}
				subq, err := Query(sub, mappings, analyzers)
				if err != nil {
					return nil, errors.New(errors.ErrorTypeParsingException, "[dis_max] failed to parse field").Cause(err)
				}
				disMaxQuery.AddShould(subq)
				queries++
			}
		case "tie_breaker":
			// parsed and validated, but not applied: see the scoring note above
			if _, err := zutils.ToFloat64(v); err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[dis_max] %s doesn't support values of type: %T", k, v))
			}
		case "boost":
			boost, err := zutils.ToFloat64(v)
			if err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[dis_max] %s doesn't support values of type: %T", k, v))
			}
			disMaxQuery.SetBoost(boost)
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[dis_max] unknown field [%s]", k))
		}
	}

	if queries == 0 {
		return nil, errors.New(errors.ErrorTypeParsingException, "[dis_max] queries must be a non-empty array")
	}
	disMaxQuery.SetMinShould(1)

	return disMaxQuery, nil
}
