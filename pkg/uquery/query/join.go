/* Copyright 2022 Zinc Labs Inc. and Contributors
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

// childScoreModes are score modes accepted by has_child query. Child scores
// are never propagated to parent, so value is validated and then ignored.
var childScoreModes = map[string]struct{}{
	"none": {}, "avg": {}, "sum": {}, "min": {}, "max": {},
}

// HasChildQuery matches child documents of join relation `type`.
//
// Parents are resolved by uquery.Rewrite before query is parsed: it replaces
// whole clause by list of `_join_parent` ids. Parsing clause directly (no
// rewrite pass) returns matching child documents themselves.
// `min_children` / `max_children` are only honoured by rewrite pass.
func HasChildQuery(query map[string]interface{}, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) (riot.Query, error) {
	var childType string
	var subq riot.Query
	for k, v := range query {
		k := strings.ToLower(k)
		switch k {
		case "type":
			t, err := zutils.ToString(v)
			if err != nil || t == "" {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[has_child] %s doesn't support values of type: %T", k, v))
			}
			childType = t
		case "query":
			q, err := subQuery("has_child", v, mappings, analyzers)
			if err != nil {
				return nil, err
			}
			subq = q
		case "score_mode":
			mode, err := zutils.ToString(v)
			if err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[has_child] %s doesn't support values of type: %T", k, v))
			}
			if _, ok := childScoreModes[strings.ToLower(mode)]; !ok {
				return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[has_child] unknown score_mode [%s]", mode))
			}
		case "min_children", "max_children":
			if _, err := zutils.ToInt(v); err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[has_child] %s doesn't support values of type: %T", k, v))
			}
		case "ignore_unmapped", "boost", "inner_hits":
			// accepted, no effect
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[has_child] unknown field [%s]", k))
		}
	}

	if childType == "" {
		return nil, errors.New(errors.ErrorTypeParsingException, "[has_child] requires [type] query")
	}
	if subq == nil {
		return nil, errors.New(errors.ErrorTypeParsingException, "[has_child] requires [query] query")
	}

	return taggedQuery(subq, meta.JoinNameField, childType), nil
}

// HasParentQuery matches parent documents of join relation `parent_type`.
//
// Children are resolved by uquery.Rewrite before query is parsed: it replaces
// whole clause by terms query on `_join_parent`. Parsing clause directly (no
// rewrite pass) returns matching parent documents themselves.
func HasParentQuery(query map[string]interface{}, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) (riot.Query, error) {
	var parentType string
	var subq riot.Query
	for k, v := range query {
		k := strings.ToLower(k)
		switch k {
		case "parent_type", "type":
			t, err := zutils.ToString(v)
			if err != nil || t == "" {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[has_parent] %s doesn't support values of type: %T", k, v))
			}
			parentType = t
		case "query":
			q, err := subQuery("has_parent", v, mappings, analyzers)
			if err != nil {
				return nil, err
			}
			subq = q
		case "score":
			if _, err := zutils.ToBool(v); err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[has_parent] %s doesn't support values of type: %T", k, v))
			}
		case "ignore_unmapped", "boost", "inner_hits":
			// accepted, no effect
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[has_parent] unknown field [%s]", k))
		}
	}

	if parentType == "" {
		return nil, errors.New(errors.ErrorTypeParsingException, "[has_parent] requires [parent_type] query")
	}
	if subq == nil {
		return nil, errors.New(errors.ErrorTypeParsingException, "[has_parent] requires [query] query")
	}

	return taggedQuery(subq, meta.JoinNameField, parentType), nil
}

// ParentIDQuery matches child documents of relation `type` that point to
// given parent `id`. It needs no rewrite pass, hidden join fields hold
// everything query requires.
func ParentIDQuery(query map[string]interface{}) (riot.Query, error) {
	var childType, id string
	for k, v := range query {
		k := strings.ToLower(k)
		switch k {
		case "type":
			t, err := zutils.ToString(v)
			if err != nil || t == "" {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[parent_id] %s doesn't support values of type: %T", k, v))
			}
			childType = t
		case "id":
			t, err := zutils.ToString(v)
			if err != nil || t == "" {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[parent_id] %s doesn't support values of type: %T", k, v))
			}
			id = t
		case "ignore_unmapped", "boost":
			// accepted, no effect
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[parent_id] unknown field [%s]", k))
		}
	}

	if childType == "" {
		return nil, errors.New(errors.ErrorTypeParsingException, "[parent_id] requires [type] query")
	}
	if id == "" {
		return nil, errors.New(errors.ErrorTypeParsingException, "[parent_id] requires [id] query")
	}

	q := riot.NewBooleanQuery()
	q.AddMust(riot.NewTermQuery(childType).SetField(meta.JoinNameField))
	q.AddMust(riot.NewTermQuery(id).SetField(meta.JoinParentField))
	return q, nil
}

// subQuery parses inner query object of join query.
func subQuery(name string, v interface{}, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) (riot.Query, error) {
	vv, ok := v.(map[string]interface{})
	if !ok {
		return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[%s] query doesn't support values of type: %T", name, v))
	}
	q, err := Query(vv, mappings, analyzers)
	if err != nil {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] failed to parse field", name)).Cause(err)
	}
	return q, nil
}
