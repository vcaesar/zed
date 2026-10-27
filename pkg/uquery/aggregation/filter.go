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

package aggregation

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/vcaesar/riot/search"

	zincaggregation "github.com/zincsearch/zincsearch/pkg/bluge/aggregation"
	"github.com/zincsearch/zincsearch/pkg/errors"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

// FilterPredicate compiles the query of a [filter] / [filters] / [nested]
// aggregation into a predicate over doc values.
//
// Aggregation calculators only see a search.DocumentMatch, they cannot run a
// searcher, so only the query types that can be answered from doc values are
// supported: term, terms, match, range, exists, match_all, match_none and
// bool combinations of those. Everything else is rejected.
//
// Deviations from elasticsearch: [match] is not analyzed, it compares the
// query string with the indexed doc values of the field, and [range] only
// supports numeric and date fields.
func FilterPredicate(name string, query interface{}, mappings *meta.Mappings) (*zincaggregation.FilterPredicate, error) {
	q, err := queryMap(name, query)
	if err != nil {
		return nil, err
	}
	if len(q) == 0 {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] aggregation query is empty", name))
	}

	preds := make([]*zincaggregation.FilterPredicate, 0, len(q))
	for _, clause := range sortedKeys(q) {
		pred, err := clausePredicate(name, clause, q[clause], mappings)
		if err != nil {
			return nil, err
		}
		preds = append(preds, pred)
	}
	return andPredicates(preds), nil
}

// queryMap normalizes a query fragment into a generic map. The zinc query DSL
// keeps raw json here, but callers may also pass typed structs.
func queryMap(name string, query interface{}) (map[string]interface{}, error) {
	if query == nil {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] aggregation needs a query", name))
	}
	if q, ok := query.(map[string]interface{}); ok {
		return q, nil
	}
	data, err := json.Marshal(query)
	if err != nil {
		return nil, errors.New(errors.ErrorTypeXContentParseException,
			fmt.Sprintf("[%s] aggregation query parse err %s", name, err.Error()))
	}
	q := make(map[string]interface{})
	if err := json.Unmarshal(data, &q); err != nil {
		return nil, errors.New(errors.ErrorTypeXContentParseException,
			fmt.Sprintf("[%s] aggregation query parse err %s", name, err.Error()))
	}
	return q, nil
}

func clausePredicate(name, clause string, v interface{}, mappings *meta.Mappings) (*zincaggregation.FilterPredicate, error) {
	switch clause {
	case "match_all":
		return &zincaggregation.FilterPredicate{Match: func(*search.DocumentMatch) bool { return true }}, nil
	case "match_none":
		return &zincaggregation.FilterPredicate{Match: func(*search.DocumentMatch) bool { return false }}, nil
	case "exists":
		return existsPredicate(name, v)
	case "term", "match":
		return termPredicate(name, clause, v, mappings)
	case "terms":
		return termsPredicate(name, v, mappings)
	case "range":
		return rangePredicate(name, v, mappings)
	case "bool":
		return boolPredicate(name, v, mappings)
	default:
		return nil, errors.New(
			errors.ErrorTypeNotImplemented,
			fmt.Sprintf("[%s] aggregation doesn't support [%s] query, only doc value queries are supported", name, clause),
		)
	}
}

// andPredicates combines predicates, all of them must match.
func andPredicates(preds []*zincaggregation.FilterPredicate) *zincaggregation.FilterPredicate {
	if len(preds) == 1 {
		return preds[0]
	}
	rv := &zincaggregation.FilterPredicate{}
	for _, p := range preds {
		rv.Fields = append(rv.Fields, p.Fields...)
	}
	rv.Match = func(d *search.DocumentMatch) bool {
		for _, p := range preds {
			if !p.Match(d) {
				return false
			}
		}
		return true
	}
	return rv
}

func existsPredicate(name string, v interface{}) (*zincaggregation.FilterPredicate, error) {
	q, ok := v.(map[string]interface{})
	if !ok {
		return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[%s] aggregation exists query format error", name))
	}
	field, ok := q["field"].(string)
	if !ok || field == "" {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] aggregation exists query needs a field", name))
	}
	return &zincaggregation.FilterPredicate{
		Fields: []string{field},
		Match:  func(d *search.DocumentMatch) bool { return len(search.Field(field).Values(d)) > 0 },
	}, nil
}

// termPredicate handles {"term": {"field": "v"}} and its long form
// {"term": {"field": {"value": "v"}}}; [match] uses "query" instead of "value".
func termPredicate(name, clause string, v interface{}, mappings *meta.Mappings) (*zincaggregation.FilterPredicate, error) {
	field, raw, err := singleField(name, clause, v)
	if err != nil {
		return nil, err
	}
	key := "value"
	if clause == "match" {
		key = "query"
	}
	if opts, ok := raw.(map[string]interface{}); ok {
		raw, ok = opts[key]
		if !ok {
			return nil, errors.New(errors.ErrorTypeParsingException,
				fmt.Sprintf("[%s] aggregation %s query needs %s", name, clause, key))
		}
	}
	return valuesPredicate(name, field, []interface{}{raw}, mappings)
}

// termsPredicate handles {"terms": {"field": ["a", "b"]}}.
func termsPredicate(name string, v interface{}, mappings *meta.Mappings) (*zincaggregation.FilterPredicate, error) {
	field, raw, err := singleField(name, "terms", v)
	if err != nil {
		return nil, err
	}
	values, ok := raw.([]interface{})
	if !ok {
		values = []interface{}{raw}
	}
	if len(values) == 0 {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] aggregation terms query needs values", name))
	}
	return valuesPredicate(name, field, values, mappings)
}

// valuesPredicate matches a document whose field holds any of values.
func valuesPredicate(name, field string, values []interface{}, mappings *meta.Mappings) (*zincaggregation.FilterPredicate, error) {
	prop, _ := mappings.GetProperty(field)
	switch prop.Type {
	case "numeric", "bool", "boolean", "date", "time":
		nums := make([]float64, 0, len(values))
		for _, v := range values {
			n, err := toQueryNumber(v, prop)
			if err != nil {
				return nil, errors.New(errors.ErrorTypeIllegalArgumentException,
					fmt.Sprintf("[%s] aggregation query value for [%s] %s", name, field, err.Error()))
			}
			nums = append(nums, n)
		}
		return &zincaggregation.FilterPredicate{
			Fields: []string{field},
			Match: func(d *search.DocumentMatch) bool {
				for _, v := range search.Field(field).Numbers(d) {
					for _, n := range nums {
						if v == n {
							return true
						}
					}
				}
				return false
			},
		}, nil
	default:
		terms := make([]string, 0, len(values))
		for _, v := range values {
			terms = append(terms, fmt.Sprint(v))
		}
		return &zincaggregation.FilterPredicate{
			Fields: []string{field},
			Match: func(d *search.DocumentMatch) bool {
				for _, v := range search.Field(field).Values(d) {
					for _, term := range terms {
						if string(v) == term {
							return true
						}
					}
				}
				return false
			},
		}, nil
	}
}

// rangePredicate handles {"range": {"field": {"gte": 1, "lt": 10}}} on numeric
// and date fields.
func rangePredicate(name string, v interface{}, mappings *meta.Mappings) (*zincaggregation.FilterPredicate, error) {
	field, raw, err := singleField(name, "range", v)
	if err != nil {
		return nil, err
	}
	bounds, ok := raw.(map[string]interface{})
	if !ok {
		return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[%s] aggregation range query format error", name))
	}
	prop, _ := mappings.GetProperty(field)
	switch prop.Type {
	case "numeric", "date", "time":
	default:
		return nil, errors.New(errors.ErrorTypeNotImplemented,
			fmt.Sprintf("[%s] aggregation range query doesn't support values of type: [%s:[%s]]", name, field, prop.Type))
	}

	type check struct {
		bound float64
		ok    func(v, bound float64) bool
	}
	ops := map[string]func(v, bound float64) bool{
		"gt":  func(v, bound float64) bool { return v > bound },
		"gte": func(v, bound float64) bool { return v >= bound },
		"lt":  func(v, bound float64) bool { return v < bound },
		"lte": func(v, bound float64) bool { return v <= bound },
	}
	checks := make([]check, 0, len(ops))
	for _, key := range sortedKeys(bounds) {
		op, ok := ops[key]
		if !ok {
			continue // boost, format, time_zone
		}
		n, err := toQueryNumber(bounds[key], prop)
		if err != nil {
			return nil, errors.New(errors.ErrorTypeIllegalArgumentException,
				fmt.Sprintf("[%s] aggregation range value for [%s] %s", name, field, err.Error()))
		}
		checks = append(checks, check{bound: n, ok: op})
	}
	if len(checks) == 0 {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] aggregation range query needs a bound", name))
	}

	return &zincaggregation.FilterPredicate{
		Fields: []string{field},
		Match: func(d *search.DocumentMatch) bool {
			for _, v := range search.Field(field).Numbers(d) {
				inRange := true
				for _, c := range checks {
					if !c.ok(v, c.bound) {
						inRange = false
						break
					}
				}
				if inRange {
					return true
				}
			}
			return false
		},
	}, nil
}

func boolPredicate(name string, v interface{}, mappings *meta.Mappings) (*zincaggregation.FilterPredicate, error) {
	q, ok := v.(map[string]interface{})
	if !ok {
		return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[%s] aggregation bool query format error", name))
	}

	must, err := subPredicates(name, q["must"], mappings)
	if err != nil {
		return nil, err
	}
	filter, err := subPredicates(name, q["filter"], mappings)
	if err != nil {
		return nil, err
	}
	must = append(must, filter...)
	mustNot, err := subPredicates(name, q["must_not"], mappings)
	if err != nil {
		return nil, err
	}
	should, err := subPredicates(name, q["should"], mappings)
	if err != nil {
		return nil, err
	}

	minimumShouldMatch := 0
	if len(should) > 0 && len(must) == 0 {
		minimumShouldMatch = 1
	}
	if v, ok := q["minimum_should_match"]; ok {
		n, err := toFloat(v)
		if err != nil {
			return nil, errors.New(errors.ErrorTypeParsingException,
				fmt.Sprintf("[%s] aggregation bool minimum_should_match must be numeric", name))
		}
		minimumShouldMatch = int(n)
	}

	rv := &zincaggregation.FilterPredicate{}
	for _, preds := range [][]*zincaggregation.FilterPredicate{must, mustNot, should} {
		for _, p := range preds {
			rv.Fields = append(rv.Fields, p.Fields...)
		}
	}
	rv.Match = func(d *search.DocumentMatch) bool {
		for _, p := range must {
			if !p.Match(d) {
				return false
			}
		}
		for _, p := range mustNot {
			if p.Match(d) {
				return false
			}
		}
		matched := 0
		for _, p := range should {
			if p.Match(d) {
				matched++
			}
		}
		return matched >= minimumShouldMatch
	}
	return rv, nil
}

// subPredicates compiles a bool clause, which is either a single query or an
// array of queries.
func subPredicates(name string, v interface{}, mappings *meta.Mappings) ([]*zincaggregation.FilterPredicate, error) {
	if v == nil {
		return nil, nil
	}
	queries, ok := v.([]interface{})
	if !ok {
		queries = []interface{}{v}
	}
	rv := make([]*zincaggregation.FilterPredicate, 0, len(queries))
	for _, query := range queries {
		pred, err := FilterPredicate(name, query, mappings)
		if err != nil {
			return nil, err
		}
		rv = append(rv, pred)
	}
	return rv, nil
}

// singleField reads the {"field": value} body shared by term, terms, match and
// range, ignoring the "boost" option.
func singleField(name, clause string, v interface{}) (string, interface{}, error) {
	q, ok := v.(map[string]interface{})
	if !ok {
		return "", nil, errors.New(errors.ErrorTypeXContentParseException,
			fmt.Sprintf("[%s] aggregation %s query format error", name, clause))
	}
	for _, field := range sortedKeys(q) {
		if field == "boost" {
			continue
		}
		return field, q[field], nil
	}
	return "", nil, errors.New(errors.ErrorTypeParsingException,
		fmt.Sprintf("[%s] aggregation %s query needs a field", name, clause))
}

// toQueryNumber renders a query value the way the doc values of prop store it:
// dates as nanoseconds, booleans as 0 / 1, numbers as they are.
func toQueryNumber(v interface{}, prop meta.Property) (float64, error) {
	switch prop.Type {
	case "date", "time":
		if s, ok := v.(string); ok {
			format := time.RFC3339
			if prop.Format != "" {
				format = prop.Format
			}
			t, err := time.Parse(format, s)
			if err != nil {
				return 0, fmt.Errorf("must be a date of format %s", format)
			}
			return float64(t.UnixNano()), nil
		}
		n, err := toFloat(v)
		if err != nil {
			return 0, fmt.Errorf("must be a date or a timestamp")
		}
		return n * float64(time.Millisecond), nil // epoch millis
	case "bool", "boolean":
		switch v := v.(type) {
		case bool:
			if v {
				return 1, nil
			}
			return 0, nil
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return 0, fmt.Errorf("must be a boolean")
			}
			if b {
				return 1, nil
			}
			return 0, nil
		}
		return 0, fmt.Errorf("must be a boolean")
	default:
		n, err := toFloat(v)
		if err != nil {
			return 0, fmt.Errorf("must be numeric")
		}
		return n, nil
	}
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
