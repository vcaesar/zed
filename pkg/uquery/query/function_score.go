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
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/search"

	"github.com/zincsearch/zincsearch/pkg/errors"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils"
)

// defaultMaxBoost matches Elasticsearch default (Float.MAX_VALUE).
const defaultMaxBoost = math.MaxFloat32

// scoreFunction is single function of function_score query.
type scoreFunction struct {
	filter    riot.Query
	weight    float64
	hasWeight bool
	// compute returns function value for document, nil means plain weight.
	compute func(dm *search.DocumentMatch) float64
}

// FunctionScoreQuery modifies score of wrapped query with one or more scoring
// functions: weight, field_value_factor, random_score and decay functions
// (gauss, exp, linear). `script_score` is not supported.
func FunctionScoreQuery(query map[string]interface{}, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) (riot.Query, error) {
	q := &functionScoreQuery{
		boost:     1.0,
		maxBoost:  defaultMaxBoost,
		scoreMode: "multiply",
		boostMode: "multiply",
	}

	inline := make(map[string]interface{})
	for k, v := range query {
		key := strings.ToLower(k)
		switch key {
		case "query":
			vv, ok := v.(map[string]interface{})
			if !ok {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] %s doesn't support values of type: %T", key, v))
			}
			subq, err := Query(vv, mappings, analyzers)
			if err != nil {
				return nil, errors.New(errors.ErrorTypeParsingException, "[function_score] failed to parse field").Cause(err)
			}
			q.inner = subq
		case "functions":
			list, ok := v.([]interface{})
			if !ok {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] %s doesn't support values of type: %T", key, v))
			}
			for _, item := range list {
				fn, ok := item.(map[string]interface{})
				if !ok {
					return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] functions doesn't support values of type: %T", item))
				}
				f, fields, err := parseScoreFunction(fn, mappings, analyzers)
				if err != nil {
					return nil, err
				}
				q.functions = append(q.functions, f)
				q.fields = append(q.fields, fields...)
			}
		case "score_mode":
			mode, err := zutils.ToString(v)
			if err != nil || !isScoreMode(mode) {
				return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] unknown score_mode [%v]", v))
			}
			q.scoreMode = strings.ToLower(mode)
		case "boost_mode":
			mode, err := zutils.ToString(v)
			if err != nil || !isBoostMode(mode) {
				return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] unknown boost_mode [%v]", v))
			}
			q.boostMode = strings.ToLower(mode)
		case "max_boost":
			f, err := zutils.ToFloat64(v)
			if err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] %s doesn't support values of type: %T", key, v))
			}
			q.maxBoost = f
		case "min_score":
			f, err := zutils.ToFloat64(v)
			if err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] %s doesn't support values of type: %T", key, v))
			}
			q.minScore = f
			q.hasMinScore = true
		case "boost":
			f, err := zutils.ToFloat64(v)
			if err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] %s doesn't support values of type: %T", key, v))
			}
			q.boost = f
		case "weight", "field_value_factor", "random_score", "gauss", "exp", "linear":
			inline[key] = v
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] unknown field [%s]", k))
		}
	}

	if len(inline) > 0 {
		f, fields, err := parseScoreFunction(inline, mappings, analyzers)
		if err != nil {
			return nil, err
		}
		q.functions = append(q.functions, f)
		q.fields = append(q.fields, fields...)
	}

	if q.inner == nil {
		subq, err := MatchAllQuery()
		if err != nil {
			return nil, err
		}
		q.inner = subq
	}
	if len(q.functions) == 0 {
		return nil, errors.New(errors.ErrorTypeParsingException, "[function_score] requires at least one function")
	}

	return q, nil
}

func isScoreMode(mode string) bool {
	switch strings.ToLower(mode) {
	case "multiply", "sum", "avg", "first", "max", "min":
		return true
	}
	return false
}

func isBoostMode(mode string) bool {
	switch strings.ToLower(mode) {
	case "multiply", "replace", "sum", "avg", "max", "min":
		return true
	}
	return false
}

// parseScoreFunction parses single entry of `functions` list, which holds
// optional filter, optional weight and at most one scoring function.
func parseScoreFunction(fn map[string]interface{}, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) (*scoreFunction, []string, error) {
	f := &scoreFunction{}
	var fields []string
	for k, v := range fn {
		key := strings.ToLower(k)
		switch key {
		case "filter":
			vv, ok := v.(map[string]interface{})
			if !ok {
				return nil, nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] %s doesn't support values of type: %T", key, v))
			}
			subq, err := Query(vv, mappings, analyzers)
			if err != nil {
				return nil, nil, errors.New(errors.ErrorTypeParsingException, "[function_score] failed to parse filter").Cause(err)
			}
			f.filter = subq
		case "weight":
			w, err := zutils.ToFloat64(v)
			if err != nil {
				return nil, nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] %s doesn't support values of type: %T", key, v))
			}
			f.weight = w
			f.hasWeight = true
		case "field_value_factor", "random_score", "gauss", "exp", "linear":
			if f.compute != nil {
				return nil, nil, errors.New(errors.ErrorTypeParsingException, "[function_score] function must hold single scoring function")
			}
			compute, field, err := parseFunctionBody(key, v, mappings)
			if err != nil {
				return nil, nil, err
			}
			f.compute = compute
			if field != "" {
				fields = append(fields, field)
			}
		default:
			return nil, nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] unknown function [%s]", k))
		}
	}

	if f.compute == nil && !f.hasWeight {
		return nil, nil, errors.New(errors.ErrorTypeParsingException, "[function_score] function requires weight or scoring function")
	}

	return f, fields, nil
}

func parseFunctionBody(name string, v interface{}, mappings *meta.Mappings) (func(dm *search.DocumentMatch) float64, string, error) {
	body, ok := v.(map[string]interface{})
	if !ok {
		return nil, "", errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] %s doesn't support values of type: %T", name, v))
	}
	switch name {
	case "field_value_factor":
		return parseFieldValueFactor(body)
	case "random_score":
		return parseRandomScore(body)
	default:
		return parseDecayFunction(name, body, mappings)
	}
}

func parseFieldValueFactor(body map[string]interface{}) (func(dm *search.DocumentMatch) float64, string, error) {
	field := ""
	factor := 1.0
	modifier := "none"
	missing := 0.0
	for k, v := range body {
		key := strings.ToLower(k)
		var err error
		switch key {
		case "field":
			field, err = zutils.ToString(v)
		case "factor":
			factor, err = zutils.ToFloat64(v)
		case "modifier":
			modifier, err = zutils.ToString(v)
			modifier = strings.ToLower(modifier)
		case "missing":
			missing, err = zutils.ToFloat64(v)
		default:
			return nil, "", errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] field_value_factor unknown field [%s]", k))
		}
		if err != nil {
			return nil, "", errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] field_value_factor %s doesn't support values of type: %T", key, v))
		}
	}
	if field == "" {
		return nil, "", errors.New(errors.ErrorTypeParsingException, "[function_score] field_value_factor requires [field]")
	}
	if !isFieldValueModifier(modifier) {
		return nil, "", errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] field_value_factor unknown modifier [%s]", modifier))
	}

	source := search.Field(field)
	return func(dm *search.DocumentMatch) float64 {
		value := missing
		if numbers := source.Numbers(dm); len(numbers) > 0 {
			value = numbers[0]
		}
		return applyModifier(modifier, value*factor)
	}, field, nil
}

func isFieldValueModifier(modifier string) bool {
	switch modifier {
	case "none", "log", "log1p", "log2p", "ln", "ln1p", "ln2p", "square", "sqrt", "reciprocal":
		return true
	}
	return false
}

// applyModifier applies field_value_factor modifier. Values outside domain of
// modifier (log/ln of non positive value, reciprocal of zero) score 0 instead
// of returning infinity or NaN.
func applyModifier(modifier string, value float64) float64 {
	switch modifier {
	case "log":
		return safeLog(math.Log10(value), value)
	case "log1p":
		return safeLog(math.Log10(1+value), 1+value)
	case "log2p":
		return safeLog(math.Log10(2+value), 2+value)
	case "ln":
		return safeLog(math.Log(value), value)
	case "ln1p":
		return safeLog(math.Log(1+value), 1+value)
	case "ln2p":
		return safeLog(math.Log(2+value), 2+value)
	case "square":
		return value * value
	case "sqrt":
		if value < 0 {
			return 0
		}
		return math.Sqrt(value)
	case "reciprocal":
		if value == 0 {
			return 0
		}
		return 1 / value
	default:
		return value
	}
}

func safeLog(result, arg float64) float64 {
	if arg <= 0 {
		return 0
	}
	return result
}

func parseRandomScore(body map[string]interface{}) (func(dm *search.DocumentMatch) float64, string, error) {
	seed := ""
	field := ""
	for k, v := range body {
		key := strings.ToLower(k)
		var err error
		switch key {
		case "seed":
			seed, err = zutils.ToString(v)
		case "field":
			field, err = zutils.ToString(v)
		default:
			return nil, "", errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] random_score unknown field [%s]", k))
		}
		if err != nil {
			return nil, "", errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] random_score %s doesn't support values of type: %T", key, v))
		}
	}

	if field == "" {
		return func(dm *search.DocumentMatch) float64 {
			var key [8]byte
			binary.BigEndian.PutUint64(key[:], dm.Number)
			return randomScore(seed, key[:])
		}, "", nil
	}

	source := search.Field(field)
	return func(dm *search.DocumentMatch) float64 {
		return randomScore(seed, source.Value(dm))
	}, field, nil
}

// randomScore returns stable pseudo random value in [0, 1) for seed and key.
func randomScore(seed string, key []byte) float64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(key)
	const mask = 1<<53 - 1
	return float64(h.Sum64()&mask) / float64(mask+1)
}

// parseDecayFunction parses gauss / exp / linear function:
// {"gauss": {"field": {"origin": .., "scale": .., "offset": .., "decay": ..}}}
func parseDecayFunction(name string, body map[string]interface{}, mappings *meta.Mappings) (func(dm *search.DocumentMatch) float64, string, error) {
	field := ""
	var params map[string]interface{}
	for k, v := range body {
		if strings.ToLower(k) == "multi_value_mode" {
			continue // accepted, first value is always used
		}
		if field != "" {
			return nil, "", errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] %s supports single field", name))
		}
		vv, ok := v.(map[string]interface{})
		if !ok {
			return nil, "", errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] %s doesn't support values of type: %T", name, v))
		}
		field, params = k, vv
	}
	if field == "" {
		return nil, "", errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] %s requires field", name))
	}

	isDate := false
	if prop, ok := mappings.GetProperty(field); ok && (prop.Type == "date" || prop.Type == "time") {
		isDate = true
	}

	var origin, scale, offset float64
	decay := 0.5
	hasOrigin, hasScale := false, false
	for k, v := range params {
		key := strings.ToLower(k)
		var err error
		switch key {
		case "origin":
			origin, err = decayValue(v, isDate, true)
			hasOrigin = err == nil
		case "scale":
			scale, err = decayValue(v, isDate, false)
			hasScale = err == nil
		case "offset":
			offset, err = decayValue(v, isDate, false)
		case "decay":
			decay, err = zutils.ToFloat64(v)
		default:
			return nil, "", errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] %s unknown field [%s]", name, k))
		}
		if err != nil {
			return nil, "", errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[function_score] %s %s doesn't support value: %v", name, key, v))
		}
	}
	if !hasOrigin || !hasScale {
		return nil, "", errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] %s requires [origin] and [scale]", name))
	}
	if scale <= 0 {
		return nil, "", errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] %s [scale] must be positive", name))
	}
	if decay <= 0 || decay >= 1 {
		return nil, "", errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[function_score] %s [decay] must be in (0, 1)", name))
	}

	decayAt := decayFunc(name, scale, decay)
	source := search.Field(field)
	return func(dm *search.DocumentMatch) float64 {
		numbers := source.Numbers(dm)
		if len(numbers) == 0 {
			return 1 // missing field does not decay, like Elasticsearch
		}
		distance := math.Abs(numbers[0]-origin) - offset
		if distance <= 0 {
			return 1
		}
		return decayAt(distance)
	}, field, nil
}

// decayFunc returns decay of distance for given decay function.
func decayFunc(name string, scale, decay float64) func(distance float64) float64 {
	switch name {
	case "exp":
		lambda := math.Log(decay) / scale
		return func(distance float64) float64 { return math.Exp(lambda * distance) }
	case "linear":
		s := scale / (1 - decay)
		return func(distance float64) float64 { return math.Max(0, (s-distance)/s) }
	default: // gauss
		sigmaSqr := -scale * scale / (2 * math.Log(decay))
		return func(distance float64) float64 { return math.Exp(-distance * distance / (2 * sigmaSqr)) }
	}
}

// decayValue parses origin / scale / offset. Date fields work in nanoseconds:
// origin accepts date string, scale and offset accept duration such as "10d".
func decayValue(v interface{}, isDate, isOrigin bool) (float64, error) {
	if s, ok := v.(string); ok && isDate {
		if isOrigin {
			t, err := zutils.ParseTime(s, "", "")
			if err != nil {
				return 0, err
			}
			return float64(t.UnixNano()), nil
		}
		d, err := parseDuration(s)
		if err != nil {
			return 0, err
		}
		return float64(d), nil
	}
	return zutils.ToFloat64(v)
}

// parseDuration parses Elasticsearch style duration, eg "10d", "12h", "30m".
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		days, err := zutils.ToFloat64(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	return time.ParseDuration(s)
}

// functionScoreQuery is riot.Query wrapper: riot has no function score query,
// so score of every match of wrapped searcher is recomputed.
type functionScoreQuery struct {
	inner       riot.Query
	functions   []*scoreFunction
	fields      []string
	boost       float64
	maxBoost    float64
	minScore    float64
	hasMinScore bool
	scoreMode   string
	boostMode   string
}

// Query returns wrapped query.
func (q *functionScoreQuery) Query() riot.Query {
	return q.inner
}

// Boost returns boost applied to final score.
func (q *functionScoreQuery) Boost() float64 {
	return q.boost
}

func (q *functionScoreQuery) Searcher(i search.Reader, options search.SearcherOptions) (search.Searcher, error) {
	s, err := q.inner.Searcher(i, options)
	if err != nil {
		return nil, err
	}

	fs := &functionScoreSearcher{Searcher: s, query: q, filters: make([]*filterCursor, len(q.functions))}
	for idx, f := range q.functions {
		if f.filter == nil {
			continue
		}
		filterSearcher, err := f.filter.Searcher(i, options)
		if err != nil {
			if cerr := fs.Close(); cerr != nil {
				return nil, fmt.Errorf("%w; close searcher: %v", err, cerr)
			}
			return nil, err
		}
		fs.filters[idx] = &filterCursor{searcher: filterSearcher}
	}
	return fs, nil
}

// Validate delegates to wrapped query when it is validatable.
func (q *functionScoreQuery) Validate() error {
	if v, ok := q.inner.(interface{ Validate() error }); ok {
		return v.Validate()
	}
	return nil
}

// filterCursor tests membership of document in filter query by advancing
// filter searcher, which is valid because matches are visited in order.
type filterCursor struct {
	searcher search.Searcher
	number   uint64
	valid    bool
	done     bool
}

func (c *filterCursor) contains(ctx *search.Context, number uint64) (bool, error) {
	for !c.done && (!c.valid || c.number < number) {
		dm, err := c.searcher.Advance(ctx, number)
		if err != nil {
			return false, err
		}
		if dm == nil {
			c.done, c.valid = true, false
			break
		}
		c.number, c.valid = dm.Number, true
		ctx.DocumentMatchPool.Put(dm)
	}
	return c.valid && c.number == number, nil
}

// functionScoreSearcher rewrites score of every match of wrapped searcher.
type functionScoreSearcher struct {
	search.Searcher
	query   *functionScoreQuery
	filters []*filterCursor
}

func (s *functionScoreSearcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	for {
		dm, err := s.Searcher.Next(ctx)
		if err != nil || dm == nil {
			return nil, err
		}
		keep, err := s.score(ctx, dm)
		if err != nil {
			return nil, err
		}
		if keep {
			return dm, nil
		}
		ctx.DocumentMatchPool.Put(dm)
	}
}

func (s *functionScoreSearcher) Advance(ctx *search.Context, number uint64) (*search.DocumentMatch, error) {
	dm, err := s.Searcher.Advance(ctx, number)
	if err != nil || dm == nil {
		return nil, err
	}
	keep, err := s.score(ctx, dm)
	if err != nil {
		return nil, err
	}
	if keep {
		return dm, nil
	}
	ctx.DocumentMatchPool.Put(dm)
	return s.Next(ctx)
}

func (s *functionScoreSearcher) Close() error {
	err := s.Searcher.Close()
	for _, f := range s.filters {
		if f == nil {
			continue
		}
		if ferr := f.searcher.Close(); ferr != nil && err == nil {
			err = ferr
		}
	}
	return err
}

func (s *functionScoreSearcher) DocumentMatchPoolSize() int {
	size := s.Searcher.DocumentMatchPoolSize() + 1
	for _, f := range s.filters {
		if f != nil {
			size += f.searcher.DocumentMatchPoolSize() + 1
		}
	}
	return size
}

// score replaces score of match, returning false when match is dropped by
// min_score.
func (s *functionScoreSearcher) score(ctx *search.Context, dm *search.DocumentMatch) (bool, error) {
	q := s.query
	if len(q.fields) > 0 {
		if err := dm.LoadDocumentValues(ctx, q.fields); err != nil {
			return false, err
		}
	}

	var combined, weightSum float64
	count := 0
	for idx, f := range q.functions {
		if cursor := s.filters[idx]; cursor != nil {
			ok, err := cursor.contains(ctx, dm.Number)
			if err != nil {
				return false, err
			}
			if !ok {
				continue
			}
		}
		value := 1.0
		if f.compute != nil {
			value = f.compute(dm)
		}
		weight := 1.0
		if f.hasWeight {
			value *= f.weight
			weight = f.weight
		}
		combined = combineFunction(q.scoreMode, combined, value, count)
		weightSum += weight
		count++
	}

	// Elasticsearch scores a document no function matched with factor 1, and
	// averages sum and avg over the sum of the weights, not over the number
	// of functions.
	factor := 1.0
	if count > 0 {
		switch q.scoreMode {
		case "avg":
			if weightSum != 0 {
				factor = combined / weightSum
			}
		case "sum":
			if weightSum != 0 {
				factor = combined
			}
		default:
			factor = combined
		}
	}

	score := combineBoost(q.boostMode, dm.Score, math.Min(factor, q.maxBoost))
	score *= q.boost

	if q.hasMinScore && score < q.minScore {
		return false, nil
	}
	if dm.Explanation != nil {
		dm.Explanation = search.NewExplanation(score, "function score", dm.Explanation)
	}
	dm.Score = score
	return true, nil
}

// combineFunction merges function value into accumulator per score_mode.
func combineFunction(mode string, acc, value float64, count int) float64 {
	if count == 0 {
		return value
	}
	switch mode {
	case "sum", "avg":
		return acc + value
	case "first":
		return acc
	case "max":
		return math.Max(acc, value)
	case "min":
		return math.Min(acc, value)
	default: // multiply
		return acc * value
	}
}

// combineBoost merges query score with function score per boost_mode.
func combineBoost(mode string, queryScore, functionScore float64) float64 {
	switch mode {
	case "replace":
		return functionScore
	case "sum":
		return queryScore + functionScore
	case "avg":
		return (queryScore + functionScore) / 2
	case "max":
		return math.Max(queryScore, functionScore)
	case "min":
		return math.Min(queryScore, functionScore)
	default: // multiply
		return queryScore * functionScore
	}
}
