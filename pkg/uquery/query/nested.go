/* Copyright 2026 AtomAI
*
* Licensed under Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with License.
* You may obtain copy of License at
*
* http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under License is distributed on "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See License for specific language governing permissions and
* limitations under License.
 */

package query

import (
	"fmt"
	"strings"

	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis"

	"github.com/zincsearch/zincsearch/pkg/errors"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils"
)

// nestedScoreModes are score modes accepted by nested query. Nested documents
// are indexed as hidden documents, their scores are never propagated to root
// document, so value is validated and then ignored.
var nestedScoreModes = map[string]struct{}{
	"avg": {}, "sum": {}, "min": {}, "max": {}, "none": {},
}

// NestedQuery matches hidden documents of nested property, ie documents
// tagged with `_nested_path` equal to `path`.
//
// Root documents are resolved by uquery.Rewrite before query is parsed: it
// replaces whole clause by list of `_nested_parent` ids. Parsing clause
// directly (no rewrite pass) therefore returns hidden nested documents
// themselves, which is what nested aggregations and sub searches need.
func NestedQuery(query map[string]interface{}, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) (riot.Query, error) {
	var path string
	var subq riot.Query
	for k, v := range query {
		k := strings.ToLower(k)
		switch k {
		case "path":
			p, err := zutils.ToString(v)
			if err != nil || p == "" {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[nested] %s doesn't support values of type: %T", k, v))
			}
			path = p
		case "query":
			vv, ok := v.(map[string]interface{})
			if !ok {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[nested] %s doesn't support values of type: %T", k, v))
			}
			q, err := Query(vv, mappings, analyzers)
			if err != nil {
				return nil, errors.New(errors.ErrorTypeParsingException, "[nested] failed to parse field").Cause(err)
			}
			subq = q
		case "score_mode":
			mode, err := zutils.ToString(v)
			if err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[nested] %s doesn't support values of type: %T", k, v))
			}
			if _, ok := nestedScoreModes[strings.ToLower(mode)]; !ok {
				return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[nested] unknown score_mode [%s]", mode))
			}
		case "ignore_unmapped", "boost", "inner_hits":
			// accepted, no effect
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[nested] unknown field [%s]", k))
		}
	}

	if path == "" {
		return nil, errors.New(errors.ErrorTypeParsingException, "[nested] requires [path] query")
	}
	if subq == nil {
		return nil, errors.New(errors.ErrorTypeParsingException, "[nested] requires [query] query")
	}

	return taggedQuery(subq, meta.NestedPathField, path), nil
}

// taggedQuery restricts query to documents having hidden field set to value.
func taggedQuery(subq riot.Query, field, value string) riot.Query {
	q := riot.NewBooleanQuery()
	q.AddMust(subq)
	q.AddMust(riot.NewTermQuery(value).SetField(field))
	return q
}
