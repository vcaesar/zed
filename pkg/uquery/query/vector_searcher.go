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

	"github.com/RoaringBitmap/roaring/v2/roaring64"
	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/knn"
)

// knnQuery is a riot.KNNQuery with an Elasticsearch pre-filter and minimum
// similarity. Unlike a bool filter around knn, the pre-filter restricts the
// candidates, so up to k filtered documents are still returned.
type knnQuery struct {
	knn        *riot.KNNQuery
	filter     riot.Query // nil: no pre-filter
	similarity *float64   // nil: no minimum similarity
}

func (q *knnQuery) Searcher(r search.Reader, options search.SearcherOptions) (search.Searcher, error) {
	s := &knnSearcher{query: q, boost: q.knn.Boost()}
	var accept func(uint64) bool
	if q.filter != nil {
		filterOptions := options
		filterOptions.Explain = false
		filter, err := q.filter.Searcher(r, filterOptions)
		if err != nil {
			return nil, err
		}
		s.filter = filter
		accept = func(n uint64) bool { return s.allowed.Contains(n) }
	}
	// inner searchers score with boost 1, so the threshold sees raw similarity
	var err error
	if s.exact, err = knn.NewSearcher(r, q.knn.Field(), q.knn.Vector(), q.knn.K(), q.knn.Metric(), 1, accept, options); err != nil {
		return nil, s.closeWith(err)
	}
	if ann := q.knn.ANN(); ann != nil {
		if s.approx, err = knn.NewApproximateSearcher(r, q.knn.Field(), q.knn.Vector(), q.knn.K(), q.knn.Metric(), *ann, 1, accept, options); err != nil {
			return nil, s.closeWith(err)
		}
	}
	return s, nil
}

type knnSearcher struct {
	query   *knnQuery
	boost   float64
	filter  search.Searcher // drained on first use
	allowed *roaring64.Bitmap
	exact   search.Searcher
	approx  search.Searcher // nil for exact search
	inner   search.Searcher // exact or approx, chosen on first use
	err     error
}

// init drains the filter and picks the searcher. Graph search over a
// selective filter walks many rejected nodes, so filters matching no more
// documents than the graph would visit use the exact scan instead.
func (s *knnSearcher) init(ctx *search.Context) error {
	if s.inner != nil || s.err != nil {
		return s.err
	}
	s.inner = s.exact
	if s.approx != nil {
		s.inner = s.approx
	}
	if s.filter == nil {
		return nil
	}
	s.allowed, s.err = drain(ctx, s.filter)
	if err := s.filter.Close(); s.err == nil {
		s.err = err
	}
	s.filter = nil
	if ann := s.query.knn.ANN(); ann != nil {
		p := ann.WithDefaults()
		if s.allowed.GetCardinality() <= uint64(max(p.EfSearch, s.query.knn.K())*p.M) {
			s.inner = s.exact
		}
	}
	return s.err
}

func drain(ctx *search.Context, filter search.Searcher) (*roaring64.Bitmap, error) {
	fctx := search.NewSearchContext(filter.DocumentMatchPoolSize(), 0)
	if ctx.Ctx != nil {
		fctx.Ctx = ctx.Ctx
	}
	allowed := roaring64.New()
	for i := 0; ; i++ {
		if i%1024 == 0 {
			if err := fctx.Ctx.Err(); err != nil {
				return nil, err
			}
		}
		dm, err := filter.Next(fctx)
		if err != nil || dm == nil {
			return allowed, err
		}
		allowed.Add(dm.Number)
		fctx.DocumentMatchPool.Put(dm)
	}
}

// keep applies the minimum similarity and the boost to a raw match.
func (s *knnSearcher) keep(ctx *search.Context, dm *search.DocumentMatch) bool {
	if min := s.query.similarity; min != nil {
		ok := dm.Score >= *min
		if s.query.knn.Metric() == riot.L2 {
			// the score is the negated squared distance, similarity the max distance
			ok = *min >= 0 && -dm.Score <= *min**min
		}
		if !ok {
			ctx.DocumentMatchPool.Put(dm)
			return false
		}
	}
	if s.boost != 1 {
		dm.Score *= s.boost
		if dm.Explanation != nil {
			dm.Explanation = search.NewExplanation(dm.Score, fmt.Sprintf("boost %f", s.boost), dm.Explanation)
		}
	}
	return true
}

func (s *knnSearcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	if err := s.init(ctx); err != nil {
		return nil, err
	}
	for {
		dm, err := s.inner.Next(ctx)
		if err != nil || dm == nil || s.keep(ctx, dm) {
			return dm, err
		}
	}
}

func (s *knnSearcher) Advance(ctx *search.Context, number uint64) (*search.DocumentMatch, error) {
	if err := s.init(ctx); err != nil {
		return nil, err
	}
	dm, err := s.inner.Advance(ctx, number)
	if err != nil || dm == nil || s.keep(ctx, dm) {
		return dm, err
	}
	return s.Next(ctx)
}

func (s *knnSearcher) Count() uint64 {
	if s.inner == nil {
		return s.exact.Count()
	}
	return s.inner.Count()
}

func (s *knnSearcher) Min() int { return 0 }

func (s *knnSearcher) Size() int { return s.exact.Size() }

func (s *knnSearcher) DocumentMatchPoolSize() int { return 1 }

func (s *knnSearcher) Close() error { return s.closeWith(nil) }

func (s *knnSearcher) closeWith(err error) error {
	if s.filter != nil {
		if cerr := s.filter.Close(); err == nil {
			err = cerr
		}
		s.filter = nil
	}
	return err
}
