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

package sql

import (
	"fmt"
	"regexp"
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"

	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// DefaultFetchSize bounds a SELECT that carries no LIMIT.
const DefaultFetchSize = 1000

// MaxBuckets bounds the number of groups a GROUP BY query considers.
const MaxBuckets = 1000

// GroupAggName is the name of the GROUP BY bucket aggregation.
const GroupAggName = "groupby"

// FieldTypes maps a field name to its zinc mapping type. Unmapped fields
// resolve to the empty string and are treated as keywords.
type FieldTypes map[string]string

func (f FieldTypes) typeOf(field string) string { return f[field] }

// calendarIntervals are the date_histogram intervals that are calendar based,
// everything else translates to a fixed_interval.
var calendarIntervals = map[string]bool{
	"second": true, "1s": true,
	"minute": true, "1m": true,
	"hour": true, "1h": true,
	"day": true, "1d": true,
	"week": true, "1w": true,
	"month": true, "1M": true,
	"quarter": true, "1q": true,
	"year": true, "1y": true,
}

// BuildDSL translates a statement into the zinc query DSL. fetchSize, when
// positive, caps the requested number of rows.
func BuildDSL(stmt *Statement, fields FieldTypes, fetchSize int) (map[string]interface{}, error) {
	query, err := whereQuery(stmt.Where, fields)
	if err != nil {
		return nil, err
	}

	size := DefaultFetchSize
	if stmt.HasLimit {
		size = stmt.Size
	}
	if fetchSize > 0 && fetchSize < size {
		size = fetchSize
	}

	dsl := map[string]interface{}{
		"query":            query,
		"track_total_hits": true,
	}
	if !stmt.Aggregated {
		dsl["from"] = stmt.Offset
		dsl["size"] = size
		if !stmt.Star {
			source := make([]interface{}, 0, len(stmt.Columns))
			for _, column := range stmt.Columns {
				source = append(source, column.Field)
			}
			dsl["_source"] = source
		}
		if sort := sortDSL(stmt); sort != nil {
			dsl["sort"] = sort
		}
		return dsl, nil
	}

	dsl["size"] = 0
	metrics := metricAggs(stmt)
	switch {
	case stmt.Group != nil:
		// Buckets are not ordered by the aggregation, so LIMIT and ORDER BY
		// are applied to the rows: every bucket has to be requested.
		bucket := groupAgg(stmt.Group, MaxBuckets)
		if len(metrics) > 0 {
			bucket["aggs"] = metrics
		}
		dsl["aggs"] = map[string]interface{}{GroupAggName: bucket}
	case len(metrics) > 0:
		dsl["aggs"] = metrics
	}
	return dsl, nil
}

// BuildQuery converts a DSL document into the query object the search
// engine consumes.
func BuildQuery(dsl map[string]interface{}) (*meta.ZincQuery, error) {
	data, err := json.Marshal(dsl)
	if err != nil {
		return nil, fmt.Errorf("marshal query DSL: %w", err)
	}
	query := new(meta.ZincQuery)
	if err := json.Unmarshal(data, query); err != nil {
		return nil, fmt.Errorf("unmarshal query DSL: %w", err)
	}
	return query, nil
}

func sortDSL(stmt *Statement) []interface{} {
	if len(stmt.Order) == 0 {
		return nil
	}
	sort := make([]interface{}, 0, len(stmt.Order))
	for _, key := range stmt.Order {
		order := "asc"
		if key.Desc {
			order = "desc"
		}
		sort = append(sort, map[string]interface{}{
			key.Label: map[string]interface{}{"order": order},
		})
	}
	return sort
}

func groupAgg(group *GroupKey, size int) map[string]interface{} {
	switch group.Kind {
	case GroupDateHistogram:
		histogram := map[string]interface{}{"field": group.Field, "size": size}
		if calendarIntervals[group.Interval] {
			histogram["calendar_interval"] = group.Interval
		} else {
			histogram["fixed_interval"] = group.Interval
		}
		return map[string]interface{}{GroupDateHistogram: histogram}
	case GroupHistogram:
		return map[string]interface{}{GroupHistogram: map[string]interface{}{
			"field":    group.Field,
			"interval": group.Step,
			"size":     size,
		}}
	default:
		return map[string]interface{}{GroupTerms: map[string]interface{}{
			"field": group.Field,
			"size":  size,
		}}
	}
}

// metricAggs builds the metric aggregations of a statement. COUNT(*) has no
// aggregation: it is the hit total, or the bucket doc_count.
func metricAggs(stmt *Statement) map[string]interface{} {
	metrics := make(map[string]interface{}, len(stmt.Columns))
	for _, column := range stmt.Columns {
		if column.Agg == AggNone || column.Agg == AggCountStar {
			continue
		}
		metrics[column.Label] = map[string]interface{}{
			column.Agg: map[string]interface{}{"field": column.Field},
		}
	}
	return metrics
}

// whereQuery translates a WHERE expression into a DSL query.
func whereQuery(expr sqlparser.Expr, fields FieldTypes) (map[string]interface{}, error) {
	if expr == nil {
		return map[string]interface{}{"match_all": map[string]interface{}{}}, nil
	}

	switch expr := expr.(type) {
	case *sqlparser.AndExpr:
		clauses, err := whereQueries(flatten(expr, andOp), fields)
		if err != nil {
			return nil, err
		}
		return boolQuery(map[string]interface{}{"must": clauses}), nil
	case *sqlparser.OrExpr:
		clauses, err := whereQueries(flatten(expr, orOp), fields)
		if err != nil {
			return nil, err
		}
		return boolQuery(map[string]interface{}{
			"should":               clauses,
			"minimum_should_match": 1,
		}), nil
	case *sqlparser.NotExpr:
		return negate(expr.Expr, fields)
	case *sqlparser.ComparisonExpr:
		return comparisonQuery(expr, fields)
	case *sqlparser.BetweenExpr:
		return betweenQuery(expr, fields)
	case *sqlparser.IsExpr:
		return isQuery(expr)
	case *sqlparser.FuncExpr:
		return funcQuery(expr)
	default:
		return nil, sqlErrorf("unsupported WHERE expression [%s]", sqlparser.String(expr))
	}
}

func whereQueries(exprs []sqlparser.Expr, fields FieldTypes) ([]interface{}, error) {
	queries := make([]interface{}, 0, len(exprs))
	for _, expr := range exprs {
		query, err := whereQuery(expr, fields)
		if err != nil {
			return nil, err
		}
		queries = append(queries, query)
	}
	return queries, nil
}

func negate(expr sqlparser.Expr, fields FieldTypes) (map[string]interface{}, error) {
	query, err := whereQuery(expr, fields)
	if err != nil {
		return nil, err
	}
	return mustNot(query), nil
}

func boolQuery(clauses map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"bool": clauses}
}

func mustNot(query map[string]interface{}) map[string]interface{} {
	return boolQuery(map[string]interface{}{"must_not": []interface{}{query}})
}

func comparisonQuery(expr *sqlparser.ComparisonExpr, fields FieldTypes) (map[string]interface{}, error) {
	field, err := columnName(expr.Left)
	if err != nil {
		return nil, sqlErrorf("unsupported WHERE expression [%s]", sqlparser.String(expr))
	}

	switch expr.Operator {
	case sqlparser.InOp, sqlparser.NotInOp:
		values, err := tupleValues(expr.Right)
		if err != nil {
			return nil, err
		}
		query := map[string]interface{}{"terms": map[string]interface{}{field: values}}
		if expr.Operator == sqlparser.NotInOp {
			return mustNot(query), nil
		}
		return query, nil
	}

	value, err := literalValue(expr.Right)
	if err != nil {
		return nil, sqlErrorf("unsupported WHERE expression [%s]", sqlparser.String(expr))
	}

	switch expr.Operator {
	case sqlparser.EqualOp:
		return equalQuery(field, value, fields), nil
	case sqlparser.NotEqualOp:
		return mustNot(equalQuery(field, value, fields)), nil
	case sqlparser.LessThanOp:
		return rangeQuery(field, map[string]interface{}{"lt": value}), nil
	case sqlparser.LessEqualOp:
		return rangeQuery(field, map[string]interface{}{"lte": value}), nil
	case sqlparser.GreaterThanOp:
		return rangeQuery(field, map[string]interface{}{"gt": value}), nil
	case sqlparser.GreaterEqualOp:
		return rangeQuery(field, map[string]interface{}{"gte": value}), nil
	case sqlparser.LikeOp, sqlparser.NotLikeOp:
		pattern, ok := value.(string)
		if !ok {
			return nil, sqlErrorf("LIKE pattern must be a string")
		}
		query := map[string]interface{}{"wildcard": map[string]interface{}{
			field: map[string]interface{}{"value": likePattern(pattern)},
		}}
		if strings.ContainsAny(pattern, "*?") {
			// the wildcard query cannot escape a literal * or ?
			query = map[string]interface{}{"regexp": map[string]interface{}{
				field: map[string]interface{}{"value": likeRegexp(pattern)},
			}}
		}
		if expr.Operator == sqlparser.NotLikeOp {
			return mustNot(query), nil
		}
		return query, nil
	default:
		return nil, sqlErrorf("unsupported operator [%s]", expr.Operator.ToString())
	}
}

// equalQuery matches analyzed text with a match query, everything else with
// an exact term query.
func equalQuery(field string, value interface{}, fields FieldTypes) map[string]interface{} {
	if fields.typeOf(field) == "text" {
		return map[string]interface{}{"match": map[string]interface{}{
			field: map[string]interface{}{"query": fmt.Sprintf("%v", value)},
		}}
	}
	return map[string]interface{}{"term": map[string]interface{}{
		field: map[string]interface{}{"value": value},
	}}
}

func rangeQuery(field string, bounds map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"range": map[string]interface{}{field: bounds}}
}

func betweenQuery(expr *sqlparser.BetweenExpr, fields FieldTypes) (map[string]interface{}, error) {
	field, err := columnName(expr.Left)
	if err != nil {
		return nil, sqlErrorf("unsupported WHERE expression [%s]", sqlparser.String(expr))
	}
	from, err := literalValue(expr.From)
	if err != nil {
		return nil, err
	}
	to, err := literalValue(expr.To)
	if err != nil {
		return nil, err
	}
	query := rangeQuery(field, map[string]interface{}{"gte": from, "lte": to})
	if !expr.IsBetween {
		return mustNot(query), nil
	}
	return query, nil
}

func isQuery(expr *sqlparser.IsExpr) (map[string]interface{}, error) {
	field, err := columnName(expr.Left)
	if err != nil {
		return nil, sqlErrorf("unsupported WHERE expression [%s]", sqlparser.String(expr))
	}
	exists := map[string]interface{}{"exists": map[string]interface{}{"field": field}}
	switch expr.Right {
	case sqlparser.IsNullOp:
		return mustNot(exists), nil
	case sqlparser.IsNotNullOp:
		return exists, nil
	default:
		return nil, sqlErrorf("unsupported WHERE expression [%s]", sqlparser.String(expr))
	}
}

// funcQuery translates the two search functions: MATCH(field, 'text') and
// QUERY('lucene syntax').
func funcQuery(expr *sqlparser.FuncExpr) (map[string]interface{}, error) {
	switch name := strings.ToLower(expr.Name.String()); name {
	case matchFuncName:
		if len(expr.Exprs) != 2 {
			return nil, sqlErrorf("MATCH requires (field, 'text')")
		}
		field, err := columnName(expr.Exprs[0])
		if err != nil {
			return nil, sqlErrorf("MATCH requires (field, 'text')")
		}
		text, err := stringArg(expr.Exprs[1])
		if err != nil {
			return nil, sqlErrorf("MATCH requires (field, 'text')")
		}
		return map[string]interface{}{"match": map[string]interface{}{
			field: map[string]interface{}{"query": text},
		}}, nil
	case queryFuncName:
		if len(expr.Exprs) != 1 {
			return nil, sqlErrorf("QUERY requires ('lucene query')")
		}
		text, err := stringArg(expr.Exprs[0])
		if err != nil {
			return nil, sqlErrorf("QUERY requires ('lucene query')")
		}
		return map[string]interface{}{"query_string": map[string]interface{}{"query": text}}, nil
	default:
		return nil, sqlErrorf("unsupported function [%s]", strings.ToUpper(name))
	}
}

func stringArg(expr sqlparser.Expr) (string, error) {
	value, err := literalValue(expr)
	if err != nil {
		return "", err
	}
	text, ok := value.(string)
	if !ok {
		return "", sqlErrorf("expected a string literal")
	}
	return text, nil
}

func tupleValues(expr sqlparser.Expr) ([]interface{}, error) {
	tuple, ok := expr.(sqlparser.ValTuple)
	if !ok {
		return nil, sqlErrorf("IN requires a list of literals")
	}
	values := make([]interface{}, 0, len(tuple))
	for _, item := range tuple {
		value, err := literalValue(item)
		if err != nil {
			return nil, sqlErrorf("IN requires a list of literals")
		}
		values = append(values, value)
	}
	return values, nil
}

// likeRegexp converts a SQL LIKE pattern to an anchored-term regexp, every
// character but the LIKE wildcards matching literally.
func likeRegexp(pattern string) string {
	var out strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '\\':
			if i+1 < len(pattern) {
				i++
			}
			out.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		case '%':
			out.WriteString(".*")
		case '_':
			out.WriteByte('.')
		default:
			out.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	return out.String()
}

// likePattern converts SQL LIKE wildcards to the wildcard query syntax.
func likePattern(pattern string) string {
	var out strings.Builder
	out.Grow(len(pattern))
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '\\':
			if i+1 < len(pattern) {
				i++
				out.WriteByte(pattern[i])
				continue
			}
			out.WriteByte(c)
		case '%':
			out.WriteByte('*')
		case '_':
			out.WriteByte('?')
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}
