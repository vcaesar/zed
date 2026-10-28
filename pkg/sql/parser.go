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

// Package sql implements the Elasticsearch SQL API: it parses a subset of
// SQL SELECT, translates it to the zed query DSL and formats the result.
package sql

import (
	"fmt"
	"strconv"
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"
)

// aggregate kinds, named after the DSL aggregation they translate to.
const (
	AggNone          = ""
	AggCountStar     = "count_star"
	AggCount         = "value_count"
	AggCountDistinct = "cardinality"
	AggSum           = "sum"
	AggAvg           = "avg"
	AggMin           = "min"
	AggMax           = "max"
)

// GROUP BY kinds, named after the DSL bucket aggregation they translate to.
const (
	GroupTerms         = "terms"
	GroupDateHistogram = "date_histogram"
	GroupHistogram     = "histogram"
)

// matchFuncName is what MATCH( is rewritten to: MATCH is a reserved word in
// the MySQL grammar and only parses as MATCH(...) AGAINST (...).
const matchFuncName = "zed_match"

// queryFuncName is the lucene escape hatch, QUERY('...').
const queryFuncName = "query"

// Error marks a SQL request zed cannot answer: a parse failure or an
// unsupported construct. Handlers render it as HTTP 400.
type Error struct {
	msg string
}

func (e *Error) Error() string { return e.msg }

func sqlErrorf(format string, args ...interface{}) *Error {
	return &Error{msg: fmt.Sprintf(format, args...)}
}

// Statement is a parsed SELECT, reduced to what the DSL can express.
type Statement struct {
	Indexes    []string
	Star       bool // SELECT *
	Columns    []Column
	Group      *GroupKey
	Where      sqlparser.Expr
	Having     []Condition
	Order      []OrderKey
	Size       int
	Offset     int
	HasLimit   bool
	Aggregated bool
}

// Column is one output column of the SELECT.
type Column struct {
	Label string // output name: the alias, or the canonical expression text
	Field string // source field, empty for COUNT(*)
	Agg   string // one of the Agg* constants
	Group bool   // column is the GROUP BY key
}

// GroupKey is the single supported GROUP BY key.
type GroupKey struct {
	Label    string
	Field    string
	Kind     string  // one of the Group* constants
	Interval string  // date_histogram interval, e.g. 1d
	Step     float64 // histogram interval
}

// Condition is one HAVING comparison against a selected aggregate.
type Condition struct {
	Label string
	Op    string
	Value float64
}

// OrderKey is one ORDER BY entry.
type OrderKey struct {
	Label string
	Desc  bool
}

// Parse turns SQL text into a Statement, rejecting unsupported constructs.
func Parse(sql string) (*Statement, error) {
	parser, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		return nil, fmt.Errorf("initialize SQL parser: %w", err)
	}
	parsed, err := parser.Parse(preprocess(sql))
	if err != nil {
		return nil, sqlErrorf("%s", err.Error())
	}
	sel, ok := parsed.(*sqlparser.Select)
	if !ok {
		return nil, sqlErrorf("only SELECT statements are supported")
	}
	return parseSelect(sel)
}

func parseSelect(sel *sqlparser.Select) (*Statement, error) {
	switch {
	case sel.Distinct:
		return nil, sqlErrorf("unsupported construct: SELECT DISTINCT")
	case sel.With != nil:
		return nil, sqlErrorf("unsupported construct: WITH")
	case len(sel.Windows) > 0:
		return nil, sqlErrorf("unsupported construct: WINDOW")
	case sel.Into != nil:
		return nil, sqlErrorf("unsupported construct: INTO")
	}

	stmt := &Statement{}
	indexes, err := parseFrom(sel.From)
	if err != nil {
		return nil, err
	}
	stmt.Indexes = indexes

	if sel.GroupBy != nil {
		if sel.GroupBy.WithRollup {
			return nil, sqlErrorf("unsupported construct: WITH ROLLUP")
		}
		if len(sel.GroupBy.Exprs) > 1 {
			return nil, sqlErrorf("unsupported construct: GROUP BY with multiple keys")
		}
		if len(sel.GroupBy.Exprs) == 1 {
			group, err := parseGroupKey(sel.GroupBy.Exprs[0])
			if err != nil {
				return nil, err
			}
			stmt.Group = group
			stmt.Aggregated = true
		}
	}

	if err := parseColumns(stmt, sel.SelectExprs); err != nil {
		return nil, err
	}
	if sel.Where != nil {
		stmt.Where = sel.Where.Expr
	}
	if sel.Having != nil {
		having, err := parseHaving(stmt, sel.Having.Expr)
		if err != nil {
			return nil, err
		}
		stmt.Having = having
	}
	if err := parseOrderBy(stmt, sel.OrderBy); err != nil {
		return nil, err
	}
	return stmt, parseLimit(stmt, sel.Limit)
}

func parseFrom(from []sqlparser.TableExpr) ([]string, error) {
	if len(from) == 0 {
		return nil, sqlErrorf("FROM is required")
	}
	indexes := make([]string, 0, len(from))
	for _, expr := range from {
		aliased, ok := expr.(*sqlparser.AliasedTableExpr)
		if !ok {
			return nil, sqlErrorf("unsupported construct: %s", sqlparser.String(expr))
		}
		table, ok := aliased.Expr.(sqlparser.TableName)
		if !ok {
			return nil, sqlErrorf("unsupported construct: %s", sqlparser.String(aliased.Expr))
		}
		name := table.Name.String()
		if name == "" {
			return nil, sqlErrorf("FROM is required")
		}
		indexes = append(indexes, name)
	}
	return indexes, nil
}

func parseColumns(stmt *Statement, exprs *sqlparser.SelectExprs) error {
	if exprs == nil || len(exprs.Exprs) == 0 {
		return sqlErrorf("SELECT requires at least one column")
	}
	for _, expr := range exprs.Exprs {
		switch expr := expr.(type) {
		case *sqlparser.StarExpr:
			stmt.Star = true
		case *sqlparser.AliasedExpr:
			column, err := parseColumn(stmt, expr)
			if err != nil {
				return err
			}
			stmt.Columns = append(stmt.Columns, column)
		default:
			return sqlErrorf("unsupported construct: %s", sqlparser.String(expr))
		}
	}

	for _, column := range stmt.Columns {
		if column.Agg != AggNone {
			stmt.Aggregated = true
		}
	}
	if !stmt.Aggregated {
		return nil
	}
	if stmt.Star {
		return sqlErrorf("unsupported construct: SELECT * with aggregates")
	}
	for _, column := range stmt.Columns {
		if column.Agg == AggNone && !column.Group {
			return sqlErrorf("column [%s] must appear in GROUP BY or be an aggregate", column.Label)
		}
	}
	return nil
}

func parseColumn(stmt *Statement, expr *sqlparser.AliasedExpr) (Column, error) {
	alias := expr.As.String()
	column, err := parseColumnExpr(stmt, expr.Expr)
	if err != nil {
		return column, err
	}
	if alias != "" {
		column.Label = alias
	}
	return column, nil
}

func parseColumnExpr(stmt *Statement, expr sqlparser.Expr) (Column, error) {
	switch expr := expr.(type) {
	case *sqlparser.ColName:
		field := expr.Name.String()
		if stmt.Group != nil && stmt.Group.Kind == GroupTerms && stmt.Group.Field == field {
			return Column{Label: stmt.Group.Label, Field: field, Group: true}, nil
		}
		return Column{Label: field, Field: field}, nil
	case *sqlparser.CountStar:
		return Column{Label: metricLabel(AggCountStar, ""), Agg: AggCountStar}, nil
	case *sqlparser.Count:
		if len(expr.Args) != 1 {
			return Column{}, sqlErrorf("unsupported construct: %s", sqlparser.String(expr))
		}
		field, err := columnName(expr.Args[0])
		if err != nil {
			return Column{}, err
		}
		agg := AggCount
		if expr.Distinct {
			agg = AggCountDistinct
		}
		return Column{Label: metricLabel(agg, field), Field: field, Agg: agg}, nil
	case *sqlparser.Sum:
		return metricColumn(AggSum, expr.Arg)
	case *sqlparser.Avg:
		return metricColumn(AggAvg, expr.Arg)
	case *sqlparser.Min:
		return metricColumn(AggMin, expr.Arg)
	case *sqlparser.Max:
		return metricColumn(AggMax, expr.Arg)
	case *sqlparser.FuncExpr:
		group, err := parseGroupFunc(expr)
		if err != nil {
			return Column{}, err
		}
		if stmt.Group == nil || stmt.Group.Label != group.Label {
			return Column{}, sqlErrorf("column [%s] must appear in GROUP BY or be an aggregate", group.Label)
		}
		return Column{Label: group.Label, Field: group.Field, Group: true}, nil
	default:
		return Column{}, sqlErrorf("unsupported construct: %s", sqlparser.String(expr))
	}
}

func metricColumn(agg string, arg sqlparser.Expr) (Column, error) {
	field, err := columnName(arg)
	if err != nil {
		return Column{}, err
	}
	return Column{Label: metricLabel(agg, field), Field: field, Agg: agg}, nil
}

func metricLabel(agg, field string) string {
	switch agg {
	case AggCountStar:
		return "COUNT(*)"
	case AggCount:
		return "COUNT(" + field + ")"
	case AggCountDistinct:
		return "COUNT(DISTINCT " + field + ")"
	default:
		return strings.ToUpper(agg) + "(" + field + ")"
	}
}

func columnName(expr sqlparser.Expr) (string, error) {
	column, ok := expr.(*sqlparser.ColName)
	if !ok {
		return "", sqlErrorf("unsupported construct: %s", sqlparser.String(expr))
	}
	return column.Name.String(), nil
}

func parseGroupKey(expr sqlparser.Expr) (*GroupKey, error) {
	switch expr := expr.(type) {
	case *sqlparser.ColName:
		field := expr.Name.String()
		return &GroupKey{Label: field, Field: field, Kind: GroupTerms}, nil
	case *sqlparser.FuncExpr:
		return parseGroupFunc(expr)
	default:
		return nil, sqlErrorf("unsupported construct: GROUP BY %s", sqlparser.String(expr))
	}
}

func parseGroupFunc(expr *sqlparser.FuncExpr) (*GroupKey, error) {
	name := strings.ToLower(expr.Name.String())
	if name != GroupDateHistogram && name != GroupHistogram {
		return nil, sqlErrorf("unsupported construct: %s", sqlparser.String(expr))
	}
	if len(expr.Exprs) != 2 {
		return nil, sqlErrorf("%s requires (field, interval)", strings.ToUpper(name))
	}
	field, err := columnName(expr.Exprs[0])
	if err != nil {
		return nil, err
	}
	value, err := literalValue(expr.Exprs[1])
	if err != nil {
		return nil, err
	}
	if name == GroupDateHistogram {
		interval, ok := value.(string)
		if !ok {
			return nil, sqlErrorf("DATE_HISTOGRAM interval must be a string, such as '1d'")
		}
		return &GroupKey{
			Label:    fmt.Sprintf("DATE_HISTOGRAM(%s, '%s')", field, interval),
			Field:    field,
			Kind:     GroupDateHistogram,
			Interval: interval,
		}, nil
	}
	step, err := toFloat(value)
	if err != nil {
		return nil, sqlErrorf("HISTOGRAM interval must be a number")
	}
	return &GroupKey{
		Label: fmt.Sprintf("HISTOGRAM(%s, %s)", field, strconv.FormatFloat(step, 'f', -1, 64)),
		Field: field,
		Kind:  GroupHistogram,
		Step:  step,
	}, nil
}

func parseHaving(stmt *Statement, expr sqlparser.Expr) ([]Condition, error) {
	if !stmt.Aggregated {
		return nil, sqlErrorf("HAVING requires an aggregated query")
	}
	conditions := make([]Condition, 0, 1)
	for _, expr := range flatten(expr, andOp) {
		comparison, ok := expr.(*sqlparser.ComparisonExpr)
		if !ok {
			return nil, sqlErrorf("unsupported construct: HAVING %s", sqlparser.String(expr))
		}
		op, err := comparisonOp(comparison.Operator)
		if err != nil {
			return nil, sqlErrorf("unsupported construct: HAVING %s", sqlparser.String(expr))
		}
		column, err := parseColumnExpr(stmt, comparison.Left)
		if err != nil {
			return nil, err
		}
		label, err := havingLabel(stmt, column.Label)
		if err != nil {
			return nil, err
		}
		value, err := literalValue(comparison.Right)
		if err != nil {
			return nil, err
		}
		number, err := toFloat(value)
		if err != nil {
			return nil, sqlErrorf("HAVING [%s] must compare against a number", sqlparser.String(expr))
		}
		conditions = append(conditions, Condition{Label: label, Op: op, Value: number})
	}
	return conditions, nil
}

// havingLabel resolves a HAVING operand to a selected aggregate, by
// expression text or by alias.
func havingLabel(stmt *Statement, label string) (string, error) {
	for _, column := range stmt.Columns {
		if column.Agg != AggNone && column.Label == label {
			return column.Label, nil
		}
	}
	for _, column := range stmt.Columns {
		if column.Agg != AggNone && column.Field == label {
			return column.Label, nil
		}
	}
	return "", sqlErrorf("HAVING [%s] must reference a selected aggregate", label)
}

func comparisonOp(op sqlparser.ComparisonExprOperator) (string, error) {
	switch op {
	case sqlparser.EqualOp:
		return "=", nil
	case sqlparser.NotEqualOp:
		return "!=", nil
	case sqlparser.LessThanOp:
		return "<", nil
	case sqlparser.LessEqualOp:
		return "<=", nil
	case sqlparser.GreaterThanOp:
		return ">", nil
	case sqlparser.GreaterEqualOp:
		return ">=", nil
	default:
		return "", sqlErrorf("unsupported operator")
	}
}

func parseOrderBy(stmt *Statement, order sqlparser.OrderBy) error {
	for _, entry := range order {
		desc := entry.Direction == sqlparser.DescOrder
		if !stmt.Aggregated {
			field, err := columnName(entry.Expr)
			if err != nil {
				return sqlErrorf("unsupported construct: ORDER BY %s", sqlparser.String(entry.Expr))
			}
			// an alias sorts on the field it names
			for _, column := range stmt.Columns {
				if column.Label == field && column.Field != "" {
					field = column.Field
					break
				}
			}
			stmt.Order = append(stmt.Order, OrderKey{Label: field, Desc: desc})
			continue
		}
		column, err := parseColumnExpr(stmt, entry.Expr)
		if err != nil {
			return err
		}
		label, err := orderLabel(stmt, column.Label)
		if err != nil {
			return err
		}
		stmt.Order = append(stmt.Order, OrderKey{Label: label, Desc: desc})
	}
	return nil
}

func orderLabel(stmt *Statement, label string) (string, error) {
	for _, column := range stmt.Columns {
		if column.Label == label || ((column.Agg != AggNone || column.Group) && column.Field == label) {
			return column.Label, nil
		}
	}
	return "", sqlErrorf("ORDER BY [%s] must reference a selected column", label)
}

func parseLimit(stmt *Statement, limit *sqlparser.Limit) error {
	if limit == nil {
		return nil
	}
	if limit.Rowcount != nil {
		size, err := literalInt(limit.Rowcount)
		if err != nil {
			return sqlErrorf("LIMIT must be a positive integer")
		}
		stmt.Size = size
		stmt.HasLimit = true
	}
	if limit.Offset != nil {
		offset, err := literalInt(limit.Offset)
		if err != nil {
			return sqlErrorf("OFFSET must be a positive integer")
		}
		stmt.Offset = offset
	}
	return nil
}

func literalInt(expr sqlparser.Expr) (int, error) {
	value, err := literalValue(expr)
	if err != nil {
		return 0, err
	}
	number, ok := value.(int64)
	if !ok || number < 0 {
		return 0, sqlErrorf("expected a positive integer")
	}
	return int(number), nil
}

// literalValue converts a SQL literal to its Go value.
func literalValue(expr sqlparser.Expr) (interface{}, error) {
	switch expr := expr.(type) {
	case *sqlparser.Literal:
		switch expr.Type {
		case sqlparser.StrVal, sqlparser.DateVal, sqlparser.TimeVal, sqlparser.TimestampVal:
			return expr.Val, nil
		case sqlparser.IntVal:
			value, err := strconv.ParseInt(expr.Val, 10, 64)
			if err != nil {
				return nil, sqlErrorf("invalid integer literal [%s]", expr.Val)
			}
			return value, nil
		case sqlparser.FloatVal, sqlparser.DecimalVal:
			value, err := strconv.ParseFloat(expr.Val, 64)
			if err != nil {
				return nil, sqlErrorf("invalid number literal [%s]", expr.Val)
			}
			return value, nil
		default:
			return nil, sqlErrorf("unsupported literal [%s]", sqlparser.String(expr))
		}
	case sqlparser.BoolVal:
		return bool(expr), nil
	case *sqlparser.NullVal:
		return nil, nil
	case *sqlparser.UnaryExpr:
		if expr.Operator != sqlparser.UMinusOp {
			return nil, sqlErrorf("unsupported literal [%s]", sqlparser.String(expr))
		}
		value, err := literalValue(expr.Expr)
		if err != nil {
			return nil, err
		}
		number, err := toFloat(value)
		if err != nil {
			return nil, sqlErrorf("unsupported literal [%s]", sqlparser.String(expr))
		}
		return -number, nil
	default:
		return nil, sqlErrorf("unsupported expression [%s]", sqlparser.String(expr))
	}
}

func toFloat(value interface{}) (float64, error) {
	switch value := value.(type) {
	case int64:
		return float64(value), nil
	case float64:
		return value, nil
	case string:
		return strconv.ParseFloat(value, 64)
	default:
		return 0, sqlErrorf("expected a number")
	}
}

type boolOp int

const (
	andOp boolOp = iota
	orOp
)

// flatten collapses a chain of AND / OR expressions into its operands.
func flatten(expr sqlparser.Expr, op boolOp) []sqlparser.Expr {
	switch expr := expr.(type) {
	case *sqlparser.AndExpr:
		if op == andOp {
			return append(flatten(expr.Left, op), flatten(expr.Right, op)...)
		}
	case *sqlparser.OrExpr:
		if op == orOp {
			return append(flatten(expr.Left, op), flatten(expr.Right, op)...)
		}
	}
	return []sqlparser.Expr{expr}
}

// preprocess adapts Elasticsearch SQL syntax to the MySQL grammar vitess
// speaks: double quotes delimit identifiers (not strings) and MATCH(f, 'x')
// is a plain function call.
func preprocess(sql string) string {
	var out strings.Builder
	out.Grow(len(sql))
	for i := 0; i < len(sql); {
		switch c := sql[i]; {
		case c == '\'' || c == '`':
			end := scanQuoted(sql, i, c)
			out.WriteString(sql[i:end])
			i = end
		case c == '"':
			ident, end := readQuoted(sql, i, '"')
			out.WriteByte('`')
			out.WriteString(strings.ReplaceAll(ident, "`", "``"))
			out.WriteByte('`')
			i = end
		case isWordByte(c):
			end := i
			for end < len(sql) && isWordByte(sql[end]) {
				end++
			}
			word := sql[i:end]
			if strings.EqualFold(word, "match") && nextNonSpace(sql, end) == '(' {
				out.WriteString(matchFuncName)
			} else {
				out.WriteString(word)
			}
			i = end
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// scanQuoted returns the index just past the quoted run starting at start.
func scanQuoted(sql string, start int, quote byte) int {
	for i := start + 1; i < len(sql); i++ {
		switch {
		case quote == '\'' && sql[i] == '\\' && i+1 < len(sql):
			i++
		case sql[i] == quote && i+1 < len(sql) && sql[i+1] == quote:
			i++
		case sql[i] == quote:
			return i + 1
		}
	}
	return len(sql)
}

// readQuoted returns the unescaped content of the quoted run starting at
// start, and the index just past it.
func readQuoted(sql string, start int, quote byte) (string, int) {
	var value strings.Builder
	for i := start + 1; i < len(sql); i++ {
		if sql[i] == quote {
			if i+1 < len(sql) && sql[i+1] == quote {
				value.WriteByte(quote)
				i++
				continue
			}
			return value.String(), i + 1
		}
		value.WriteByte(sql[i])
	}
	return value.String(), len(sql)
}

func nextNonSpace(sql string, i int) byte {
	for ; i < len(sql); i++ {
		switch sql[i] {
		case ' ', '\t', '\n', '\r':
		default:
			return sql[i]
		}
	}
	return 0
}

func isWordByte(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
