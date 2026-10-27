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
	"github.com/vcaesar/riot/search"

	"github.com/zincsearch/zincsearch/pkg/errors"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils"
)

// ConstantScoreQuery wraps a filter query and gives every matching document
// the same score (`boost`, default 1.0), like Elasticsearch's constant_score.
func ConstantScoreQuery(query map[string]interface{}, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) (riot.Query, error) {
	var filter riot.Query
	boost := 1.0
	for k, v := range query {
		k := strings.ToLower(k)
		switch k {
		case "filter":
			vv, ok := v.(map[string]interface{})
			if !ok {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[constant_score] %s doesn't support values of type: %T", k, v))
			}
			subq, err := Query(vv, mappings, analyzers)
			if err != nil {
				return nil, errors.New(errors.ErrorTypeParsingException, "[constant_score] failed to parse field").Cause(err)
			}
			filter = subq
		case "boost":
			f, err := zutils.ToFloat64(v)
			if err != nil {
				return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[constant_score] %s doesn't support values of type: %T", k, v))
			}
			boost = f
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[constant_score] unknown field [%s]", k))
		}
	}

	if filter == nil {
		return nil, errors.New(errors.ErrorTypeParsingException, "[constant_score] requires a [filter] query")
	}

	return &constantScoreQuery{inner: filter, boost: boost}, nil
}

// constantScoreQuery is a riot.Query wrapper: riot has no constant-score
// query, so the score is forced on the DocumentMatch of the wrapped searcher.
type constantScoreQuery struct {
	inner riot.Query
	boost float64
}

// Boost returns the constant score assigned to every matching document.
func (q *constantScoreQuery) Boost() float64 {
	return q.boost
}

// Query returns the wrapped filter query.
func (q *constantScoreQuery) Query() riot.Query {
	return q.inner
}

func (q *constantScoreQuery) Searcher(i search.Reader, options search.SearcherOptions) (search.Searcher, error) {
	s, err := q.inner.Searcher(i, options)
	if err != nil {
		return nil, err
	}
	return &constantScoreSearcher{Searcher: s, boost: q.boost}, nil
}

// Validate delegates to the wrapped query when it is validatable, so that a
// constant_score nested in a boolean query still validates its filter.
func (q *constantScoreQuery) Validate() error {
	if v, ok := q.inner.(interface{ Validate() error }); ok {
		return v.Validate()
	}
	return nil
}

// constantScoreSearcher delegates every searcher method to the wrapped
// searcher and only rewrites the score of the matches it returns.
type constantScoreSearcher struct {
	search.Searcher
	boost float64
}

func (s *constantScoreSearcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	dm, err := s.Searcher.Next(ctx)
	if dm != nil {
		dm.Score = s.boost
	}
	return dm, err
}

func (s *constantScoreSearcher) Advance(ctx *search.Context, number uint64) (*search.DocumentMatch, error) {
	dm, err := s.Searcher.Advance(ctx, number)
	if dm != nil {
		dm.Score = s.boost
	}
	return dm, err
}
