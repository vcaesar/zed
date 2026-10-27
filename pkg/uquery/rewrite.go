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

package uquery

import (
	"fmt"
	"strings"

	"github.com/zincsearch/zincsearch/pkg/config"
	"github.com/zincsearch/zincsearch/pkg/errors"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

// MaxRewriteMatches is hard limit of documents joined by single
// nested / has_child / has_parent clause.
const MaxRewriteMatches = 65536

// ExecFunc runs sub query and returns value of collectField for every matching
// document. Values are returned one per match, duplicates included, so that
// callers can count matches per parent (has_child min_children/max_children).
type ExecFunc func(sub map[string]interface{}, collectField string) ([]string, error)

// Rewrite replaces `nested`, `has_child`, `has_parent` and `parent_id` clauses
// of query DSL by plain term queries on hidden fields. Riot has no block join
// support, so joins are resolved in separate search pass (exec) and result is
// pushed back into query as list of document ids.
//
// Clauses are rewritten as:
//
//	nested{path,query}       -> terms _id           of matching _nested_parent
//	has_child{type,query}    -> terms _id           of matching _join_parent
//	has_parent{type,query}   -> terms _join_parent  of matching _id
//	parent_id{type,id}       -> _join_name + _join_parent terms
//
// `score_mode` / `score` are accepted but ignored: joined documents get
// constant score, child scores are not propagated to parent.
func Rewrite(query interface{}, exec func(sub map[string]interface{}, collectField string) ([]string, error)) (interface{}, error) {
	q, err := queryToMap(query)
	if err != nil {
		return nil, err
	}
	if q == nil {
		return query, nil
	}
	rewritten, err := rewriteQuery(q, exec)
	if err != nil {
		return nil, err
	}
	return rewritten, nil
}

// queryToMap converts supported query representations to generic map. It
// returns nil map when query holds nothing to rewrite.
func queryToMap(query interface{}) (map[string]interface{}, error) {
	switch q := query.(type) {
	case nil:
		return nil, nil
	case map[string]interface{}:
		return q, nil
	case *meta.Query:
		if q == nil {
			return nil, nil
		}
		data, err := json.Marshal(q)
		if err != nil {
			return nil, errors.New(errors.ErrorTypeInvalidArgument, "query must be map[string]interface{}")
		}
		var newQuery map[string]interface{}
		if err = json.Unmarshal(data, &newQuery); err != nil {
			return nil, errors.New(errors.ErrorTypeInvalidArgument, "query must be map[string]interface{}")
		}
		return newQuery, nil
	default:
		return nil, nil
	}
}

func rewriteQuery(q map[string]interface{}, exec ExecFunc) (map[string]interface{}, error) {
	if len(q) == 1 {
		for k, v := range q {
			switch strings.ToLower(k) {
			case "nested":
				return rewriteNested(v, exec)
			case "has_child":
				return rewriteHasChild(v, exec)
			case "has_parent":
				return rewriteHasParent(v, exec)
			case "parent_id":
				return rewriteParentID(v)
			}
		}
	}

	out := make(map[string]interface{}, len(q))
	for k, v := range q {
		var err error
		switch strings.ToLower(k) {
		case "bool":
			out[k], err = rewriteBool(v, exec)
		case "constant_score":
			out[k], err = rewriteFields(v, exec, "filter")
		case "dis_max":
			out[k], err = rewriteFields(v, exec, "queries")
		case "boosting":
			out[k], err = rewriteFields(v, exec, "positive", "negative")
		case "function_score":
			out[k], err = rewriteFunctionScore(v, exec)
		default:
			out[k] = v
		}
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

// rewriteClause rewrites single query object or array of query objects.
func rewriteClause(v interface{}, exec ExecFunc) (interface{}, error) {
	switch v := v.(type) {
	case map[string]interface{}:
		return rewriteQuery(v, exec)
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			sub, ok := item.(map[string]interface{})
			if !ok {
				out[i] = item
				continue
			}
			rewritten, err := rewriteQuery(sub, exec)
			if err != nil {
				return nil, err
			}
			out[i] = rewritten
		}
		return out, nil
	default:
		return v, nil
	}
}

func rewriteBool(v interface{}, exec ExecFunc) (interface{}, error) {
	return rewriteFields(v, exec, "must", "should", "must_not", "filter")
}

// rewriteFields rewrites listed sub query fields of clause and copies rest.
func rewriteFields(v interface{}, exec ExecFunc, fields ...string) (interface{}, error) {
	clause, ok := v.(map[string]interface{})
	if !ok {
		return v, nil
	}
	out := make(map[string]interface{}, len(clause))
	for k, sub := range clause {
		lower := strings.ToLower(k)
		rewrite := false
		for _, field := range fields {
			if lower == field {
				rewrite = true
				break
			}
		}
		if !rewrite {
			out[k] = sub
			continue
		}
		rewritten, err := rewriteClause(sub, exec)
		if err != nil {
			return nil, err
		}
		out[k] = rewritten
	}
	return out, nil
}

func rewriteFunctionScore(v interface{}, exec ExecFunc) (interface{}, error) {
	rewritten, err := rewriteFields(v, exec, "query")
	if err != nil {
		return nil, err
	}
	clause, ok := rewritten.(map[string]interface{})
	if !ok {
		return rewritten, nil
	}
	functions, ok := clause["functions"].([]interface{})
	if !ok {
		return clause, nil
	}
	newFunctions := make([]interface{}, len(functions))
	for i, item := range functions {
		fn, ok := item.(map[string]interface{})
		if !ok {
			newFunctions[i] = item
			continue
		}
		newFn, err := rewriteFields(fn, exec, "filter")
		if err != nil {
			return nil, err
		}
		newFunctions[i] = newFn
	}
	clause["functions"] = newFunctions
	return clause, nil
}

func rewriteNested(v interface{}, exec ExecFunc) (map[string]interface{}, error) {
	clause, err := clauseMap("nested", v)
	if err != nil {
		return nil, err
	}

	var path string
	var subQuery interface{}
	for k, value := range clause {
		switch strings.ToLower(k) {
		case "path":
			if path, err = zutils.ToString(value); err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[nested] path doesn't support values of type: %T", value))
			}
		case "query":
			subQuery = value
		case "score_mode", "ignore_unmapped", "boost", "inner_hits":
			// accepted, scores of nested documents are not propagated
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[nested] unknown field [%s]", k))
		}
	}
	if path == "" {
		return nil, errors.New(errors.ErrorTypeParsingException, "[nested] requires [path] field")
	}
	if subQuery == nil {
		return nil, errors.New(errors.ErrorTypeParsingException, "[nested] requires [query] field")
	}

	ids, err := exec(filteredQuery(subQuery, meta.NestedPathField, path), meta.NestedParentField)
	if err != nil {
		return nil, err
	}
	return idsQuery("nested", "_id", unique(ids))
}

func rewriteHasChild(v interface{}, exec ExecFunc) (map[string]interface{}, error) {
	clause, err := clauseMap("has_child", v)
	if err != nil {
		return nil, err
	}

	var childType string
	var subQuery interface{}
	minChildren, maxChildren := 0, 0
	for k, value := range clause {
		switch strings.ToLower(k) {
		case "type":
			if childType, err = zutils.ToString(value); err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[has_child] type doesn't support values of type: %T", value))
			}
		case "query":
			subQuery = value
		case "min_children":
			if minChildren, err = zutils.ToInt(value); err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[has_child] min_children doesn't support values of type: %T", value))
			}
		case "max_children":
			if maxChildren, err = zutils.ToInt(value); err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[has_child] max_children doesn't support values of type: %T", value))
			}
		case "score_mode", "ignore_unmapped", "boost", "inner_hits":
			// accepted, child scores are not propagated to parent
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[has_child] unknown field [%s]", k))
		}
	}
	if childType == "" {
		return nil, errors.New(errors.ErrorTypeParsingException, "[has_child] requires [type] field")
	}
	if subQuery == nil {
		return nil, errors.New(errors.ErrorTypeParsingException, "[has_child] requires [query] field")
	}

	// child documents may carry nested documents of their own
	if subQuery, err = rewriteClause(subQuery, exec); err != nil {
		return nil, err
	}
	parents, err := exec(filteredQuery(subQuery, meta.JoinNameField, childType), meta.JoinParentField)
	if err != nil {
		return nil, err
	}
	return idsQuery("has_child", "_id", limitByCount(parents, minChildren, maxChildren))
}

func rewriteHasParent(v interface{}, exec ExecFunc) (map[string]interface{}, error) {
	clause, err := clauseMap("has_parent", v)
	if err != nil {
		return nil, err
	}

	var parentType string
	var subQuery interface{}
	for k, value := range clause {
		switch strings.ToLower(k) {
		case "parent_type", "type":
			if parentType, err = zutils.ToString(value); err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[has_parent] parent_type doesn't support values of type: %T", value))
			}
		case "query":
			subQuery = value
		case "score", "ignore_unmapped", "boost", "inner_hits":
			// accepted, parent scores are not propagated to children
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[has_parent] unknown field [%s]", k))
		}
	}
	if parentType == "" {
		return nil, errors.New(errors.ErrorTypeParsingException, "[has_parent] requires [parent_type] field")
	}
	if subQuery == nil {
		return nil, errors.New(errors.ErrorTypeParsingException, "[has_parent] requires [query] field")
	}

	// parent documents may carry nested documents of their own
	if subQuery, err = rewriteClause(subQuery, exec); err != nil {
		return nil, err
	}
	ids, err := exec(filteredQuery(subQuery, meta.JoinNameField, parentType), "_id")
	if err != nil {
		return nil, err
	}
	return idsQuery("has_parent", meta.JoinParentField, unique(ids))
}

func rewriteParentID(v interface{}) (map[string]interface{}, error) {
	clause, err := clauseMap("parent_id", v)
	if err != nil {
		return nil, err
	}

	var childType, id string
	for k, value := range clause {
		switch strings.ToLower(k) {
		case "type":
			if childType, err = zutils.ToString(value); err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[parent_id] type doesn't support values of type: %T", value))
			}
		case "id":
			if id, err = zutils.ToString(value); err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[parent_id] id doesn't support values of type: %T", value))
			}
		case "ignore_unmapped", "boost":
			// accepted
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[parent_id] unknown field [%s]", k))
		}
	}
	if childType == "" {
		return nil, errors.New(errors.ErrorTypeParsingException, "[parent_id] requires [type] field")
	}
	if id == "" {
		return nil, errors.New(errors.ErrorTypeParsingException, "[parent_id] requires [id] field")
	}

	return map[string]interface{}{
		"bool": map[string]interface{}{
			"filter": []interface{}{
				map[string]interface{}{"term": map[string]interface{}{meta.JoinNameField: childType}},
				map[string]interface{}{"term": map[string]interface{}{meta.JoinParentField: id}},
			},
		},
	}, nil
}

func clauseMap(name string, v interface{}) (map[string]interface{}, error) {
	clause, ok := v.(map[string]interface{})
	if !ok {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] query doesn't support value type %T", name, v))
	}
	return clause, nil
}

// filteredQuery restricts sub query to documents tagged with given hidden field.
func filteredQuery(sub interface{}, field, value string) map[string]interface{} {
	return map[string]interface{}{
		"bool": map[string]interface{}{
			"must": sub,
			"filter": map[string]interface{}{
				"term": map[string]interface{}{field: value},
			},
		},
	}
}

// idsQuery builds terms query on field, or match_none when nothing matched.
func idsQuery(name, field string, ids []string) (map[string]interface{}, error) {
	limit := RewriteLimit()
	if len(ids) > limit {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] too many matches [%d], limit is [%d]", name, len(ids), limit))
	}
	if len(ids) == 0 {
		return map[string]interface{}{"match_none": map[string]interface{}{}}, nil
	}
	return map[string]interface{}{"terms": map[string]interface{}{field: ids}}, nil
}

// RewriteLimit returns maximum number of documents single join clause may match.
func RewriteLimit() int {
	limit := config.Global.MaxResults
	if limit <= 0 || limit > MaxRewriteMatches {
		limit = MaxRewriteMatches
	}
	return limit
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// limitByCount keeps ids matched at least minCount and at most maxCount times.
func limitByCount(values []string, minCount, maxCount int) []string {
	counts := make(map[string]int, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		counts[v]++
		if counts[v] == 1 {
			out = append(out, v)
		}
	}
	if minCount <= 1 && maxCount <= 0 {
		return out
	}
	kept := out[:0]
	for _, v := range out {
		if counts[v] < minCount {
			continue
		}
		if maxCount > 0 && counts[v] > maxCount {
			continue
		}
		kept = append(kept, v)
	}
	return kept
}
