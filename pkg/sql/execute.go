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
	"sort"
	"strings"
	"time"

	"github.com/zincsearch/zincsearch/pkg/core"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/meta/elastic"
)

// Translate parses sql and returns the query DSL it maps to.
func Translate(sql string, fetchSize int) (map[string]interface{}, error) {
	stmt, err := Parse(sql)
	if err != nil {
		return nil, err
	}
	return BuildDSL(stmt, indexFields(stmt.Indexes), fetchSize)
}

// Query parses, translates and runs sql, and returns the rows it produced.
func Query(sql string, fetchSize int) (*Response, error) {
	stmt, err := Parse(sql)
	if err != nil {
		return nil, err
	}
	fields := indexFields(stmt.Indexes)
	dsl, err := BuildDSL(stmt, fields, fetchSize)
	if err != nil {
		return nil, err
	}
	query, err := BuildQuery(dsl)
	if err != nil {
		return nil, err
	}
	resp, err := search(stmt.Indexes, query)
	if err != nil {
		return nil, err
	}
	return buildResponse(stmt, fields, resp, fetchSize)
}

func search(indexes []string, query *meta.ZincQuery) (*meta.SearchResponse, error) {
	if len(indexes) == 1 && !strings.Contains(indexes[0], "*") {
		index, exists := core.GetIndex(indexes[0])
		if !exists {
			return nil, fmt.Errorf("index %s does not exists", indexes[0])
		}
		return index.Search(query)
	}
	return core.MultiSearch(indexes, query)
}

// indexFields collects the mapping types of every index the statement reads.
func indexFields(indexes []string) FieldTypes {
	fields := make(FieldTypes)
	for _, index := range core.ZINC_INDEX_LIST.List() {
		if !matchesAny(index.GetName(), indexes) {
			continue
		}
		mappings := index.GetMappings()
		if mappings == nil {
			continue
		}
		for name, property := range mappings.ListProperty() {
			if _, ok := fields[name]; !ok {
				fields[name] = property.Type
			}
		}
	}
	return fields
}

func matchesAny(indexName string, patterns []string) bool {
	return elastic.MatchAnyPattern(indexName, patterns)
}

func buildResponse(stmt *Statement, fields FieldTypes, resp *meta.SearchResponse, fetchSize int) (*Response, error) {
	var result *Response
	var err error
	if stmt.Aggregated {
		result, err = aggregatedResponse(stmt, fields, resp)
	} else {
		result, err = hitsResponse(stmt, fields, resp)
	}
	if err != nil {
		return nil, err
	}
	if fetchSize > 0 && len(result.Rows) > fetchSize {
		result.Rows = result.Rows[:fetchSize]
	}
	return result, nil
}

func hitsResponse(stmt *Statement, fields FieldTypes, resp *meta.SearchResponse) (*Response, error) {
	sources := make([]map[string]interface{}, 0, len(resp.Hits.Hits))
	for _, hit := range resp.Hits.Hits {
		source, _ := hit.Source.(map[string]interface{})
		if source == nil {
			source = map[string]interface{}{}
		}
		sources = append(sources, source)
	}

	names := make([]string, 0, len(stmt.Columns))
	if stmt.Star {
		names = sourceFields(sources)
	}
	result := &Response{Columns: make([]ResultColumn, 0, len(stmt.Columns)+len(names))}
	for _, name := range names {
		result.Columns = append(result.Columns, ResultColumn{Name: name, Type: columnType(fields.typeOf(name))})
	}
	for _, column := range stmt.Columns {
		names = append(names, column.Field)
		result.Columns = append(result.Columns, ResultColumn{
			Name: column.Label,
			Type: columnType(fields.typeOf(column.Field)),
		})
	}

	result.Rows = make([][]interface{}, 0, len(sources))
	for _, source := range sources {
		row := make([]interface{}, 0, len(names))
		for _, name := range names {
			row = append(row, source[name])
		}
		result.Rows = append(result.Rows, row)
	}
	return result, nil
}

// sourceFields is the sorted union of the fields of the returned documents,
// used as the column list of SELECT *.
func sourceFields(sources []map[string]interface{}) []string {
	seen := make(map[string]struct{})
	for _, source := range sources {
		for name := range source {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func aggregatedResponse(stmt *Statement, fields FieldTypes, resp *meta.SearchResponse) (*Response, error) {
	result := &Response{Columns: make([]ResultColumn, 0, len(stmt.Columns))}
	for _, column := range stmt.Columns {
		result.Columns = append(result.Columns, ResultColumn{
			Name: column.Label,
			Type: aggColumnType(stmt, column, fields),
		})
	}

	rows, err := aggregatedRows(stmt, resp)
	if err != nil {
		return nil, err
	}
	rows = filterRows(stmt, result.Columns, rows)
	sortRows(stmt, result.Columns, rows)
	rows = rows[min(stmt.Offset, len(rows)):]
	if stmt.HasLimit && len(rows) > stmt.Size {
		rows = rows[:stmt.Size]
	}
	result.Rows = rows
	return result, nil
}

func aggregatedRows(stmt *Statement, resp *meta.SearchResponse) ([][]interface{}, error) {
	if stmt.Group == nil {
		row := make([]interface{}, 0, len(stmt.Columns))
		for _, column := range stmt.Columns {
			if column.Agg == AggCountStar {
				row = append(row, int64(resp.Hits.Total.Value))
				continue
			}
			row = append(row, metricValue(resp.Aggregations[column.Label], column.Agg))
		}
		return [][]interface{}{row}, nil
	}

	buckets, err := groupBuckets(resp)
	if err != nil {
		return nil, err
	}
	rows := make([][]interface{}, 0, len(buckets))
	for _, bucket := range buckets {
		row := make([]interface{}, 0, len(stmt.Columns))
		for _, column := range stmt.Columns {
			switch {
			case column.Group:
				row = append(row, groupValue(stmt.Group, bucket["key"]))
			case column.Agg == AggCountStar:
				row = append(row, toInt64(bucket["doc_count"]))
			default:
				value, _ := bucket[column.Label].(meta.AggregationResponse)
				row = append(row, metricValue(value, column.Agg))
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func groupBuckets(resp *meta.SearchResponse) ([]map[string]interface{}, error) {
	agg, ok := resp.Aggregations[GroupAggName]
	if !ok {
		return nil, nil
	}
	buckets, ok := agg.Buckets.([]map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected [%s] aggregation result: %T", GroupAggName, agg.Buckets)
	}
	return buckets, nil
}

// groupValue renders a bucket key: date histogram keys are epoch millis and
// are returned as timestamps.
func groupValue(group *GroupKey, key interface{}) interface{} {
	if group.Kind != GroupDateHistogram {
		return key
	}
	millis, ok := toFloat64(key)
	if !ok {
		return key
	}
	return time.UnixMilli(int64(millis)).UTC().Format(time.RFC3339)
}

func metricValue(agg meta.AggregationResponse, kind string) interface{} {
	if agg.Value == nil {
		return nil
	}
	if kind == AggCount || kind == AggCountDistinct {
		return toInt64(agg.Value)
	}
	return agg.Value
}

func filterRows(stmt *Statement, columns []ResultColumn, rows [][]interface{}) [][]interface{} {
	if len(stmt.Having) == 0 {
		return rows
	}
	kept := make([][]interface{}, 0, len(rows))
	for _, row := range rows {
		matched := true
		for _, condition := range stmt.Having {
			index := columnIndex(columns, condition.Label)
			if index < 0 || !matchCondition(row[index], condition) {
				matched = false
				break
			}
		}
		if matched {
			kept = append(kept, row)
		}
	}
	return kept
}

func matchCondition(value interface{}, condition Condition) bool {
	number, ok := toFloat64(value)
	if !ok {
		return false
	}
	switch condition.Op {
	case "=":
		return number == condition.Value
	case "!=":
		return number != condition.Value
	case "<":
		return number < condition.Value
	case "<=":
		return number <= condition.Value
	case ">":
		return number > condition.Value
	case ">=":
		return number >= condition.Value
	default:
		return false
	}
}

// sortRows applies ORDER BY to aggregated rows: bucket ordering cannot be
// pushed down to the aggregation.
func sortRows(stmt *Statement, columns []ResultColumn, rows [][]interface{}) {
	if len(stmt.Order) == 0 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		for _, key := range stmt.Order {
			index := columnIndex(columns, key.Label)
			if index < 0 {
				continue
			}
			cmp := compareValues(rows[i][index], rows[j][index])
			if cmp == 0 {
				continue
			}
			if key.Desc {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})
}

func columnIndex(columns []ResultColumn, label string) int {
	for i, column := range columns {
		if column.Name == label {
			return i
		}
	}
	return -1
}

func compareValues(a, b interface{}) int {
	left, leftOK := toFloat64(a)
	right, rightOK := toFloat64(b)
	if leftOK && rightOK {
		switch {
		case left < right:
			return -1
		case left > right:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(renderValue(a), renderValue(b))
}

func toFloat64(value interface{}) (float64, bool) {
	switch value := value.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint64:
		return float64(value), true
	default:
		return 0, false
	}
}

// toInt64 narrows a numeric aggregation value to an integer, as SQL long
// columns are reported.
func toInt64(value interface{}) interface{} {
	number, ok := toFloat64(value)
	if !ok {
		return nil
	}
	return int64(number)
}

// columnType maps a zinc mapping type to the SQL type reported in the
// response columns.
func columnType(mappingType string) string {
	switch mappingType {
	case "text":
		return "text"
	case "numeric":
		return "double"
	case "date", "time":
		return "datetime"
	case "bool", "boolean":
		return "boolean"
	default:
		return "keyword"
	}
}

func aggColumnType(stmt *Statement, column Column, fields FieldTypes) string {
	switch {
	case column.Group && stmt.Group.Kind == GroupDateHistogram:
		return "datetime"
	case column.Group && stmt.Group.Kind == GroupHistogram:
		return "double"
	case column.Group:
		return columnType(fields.typeOf(column.Field))
	case column.Agg == AggCountStar, column.Agg == AggCount, column.Agg == AggCountDistinct:
		return "long"
	case column.Agg != AggNone:
		return "double"
	default:
		return columnType(fields.typeOf(column.Field))
	}
}
