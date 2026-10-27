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
	"bytes"
	"sort"

	"github.com/vcaesar/riot/search"
)

// TopHit is one document kept by a [top_hits] aggregation. Source holds the
// raw _source bytes; they are copied because the stored field visitor reuses
// its buffer once Consume returns.
type TopHit struct {
	ID     string
	Index  string
	Score  float64
	Source []byte

	sortKey []byte // doc value of the sort field, nil when sorting by _score
}

// TopHitsAggregation implements the elasticsearch [top_hits] aggregation:
// the best documents of the enclosing bucket.
//
// Only a single sort field is supported; sortField "" sorts by descending
// _score, as elasticsearch does by default.
type TopHitsAggregation struct {
	size      int
	from      int
	sortField string
	sortDesc  bool

	sourceEnable bool
	sourceFields []string
}

// NewTopHitsAggregation returns the aggregation backing [top_hits].
func NewTopHitsAggregation(size, from int, sortField string, sortDesc bool) *TopHitsAggregation {
	return &TopHitsAggregation{size: size, from: from, sortField: sortField, sortDesc: sortDesc, sourceEnable: true}
}

// SetSource configures the _source filtering of the returned hits. The
// aggregation layer only carries the spec, the response layer applies it.
func (a *TopHitsAggregation) SetSource(enable bool, fields []string) {
	a.sourceEnable, a.sourceFields = enable, fields
}

func (a *TopHitsAggregation) Fields() []string {
	if a.sortField == "" {
		return nil
	}
	return []string{a.sortField}
}

func (a *TopHitsAggregation) Calculator() search.Calculator {
	return &TopHitsCalculator{
		size:         a.size,
		from:         a.from,
		sortField:    a.sortField,
		sortDesc:     a.sortDesc,
		sourceEnable: a.sourceEnable,
		sourceFields: a.sourceFields,
	}
}

// TopHitsCalculator keeps the best from+size documents of its bucket. It
// implements neither MetricCalculator nor BucketCalculator: the response layer
// type-switches on the concrete type.
type TopHitsCalculator struct {
	size      int
	from      int
	sortField string
	sortDesc  bool

	sourceEnable bool
	sourceFields []string

	hits  []TopHit
	total uint64
	err   error
}

// Source returns the _source filtering requested for the hits.
func (c *TopHitsCalculator) Source() (enable bool, fields []string) {
	return c.sourceEnable, c.sourceFields
}

func (c *TopHitsCalculator) Consume(d *search.DocumentMatch) {
	c.total++
	hit := TopHit{Score: d.Score}
	if c.sortField != "" {
		hit.sortKey = append([]byte(nil), search.Field(c.sortField).Value(d)...)
	}
	err := d.VisitStoredFields(func(field string, value []byte) bool {
		switch field {
		case "_id":
			hit.ID = string(value)
		case "_index":
			hit.Index = string(value)
		case "_source":
			hit.Source = append([]byte(nil), value...)
		}
		return true
	})
	if err != nil {
		if c.err == nil {
			c.err = err
		}
		return
	}
	c.hits = append(c.hits, hit)
}

func (c *TopHitsCalculator) Merge(other search.Calculator) {
	o, ok := other.(*TopHitsCalculator)
	if !ok {
		return
	}
	c.total += o.total
	c.hits = append(c.hits, o.hits...)
	if c.err == nil {
		c.err = o.err
	}
	c.Finish()
}

// Finish keeps the from+size best hits, so that a later Merge still has the
// documents skipped by from. Hits applies the window.
func (c *TopHitsCalculator) Finish() {
	sort.SliceStable(c.hits, c.less)
	if keep := c.from + c.size; keep > 0 && keep < len(c.hits) {
		c.hits = c.hits[:keep]
	}
}

func (c *TopHitsCalculator) less(i, j int) bool {
	if c.sortField == "" {
		return c.hits[i].Score > c.hits[j].Score // best score first
	}
	// Doc value terms of both text and numeric fields sort in byte order.
	cmp := bytes.Compare(c.hits[i].sortKey, c.hits[j].sortKey)
	if c.sortDesc {
		return cmp > 0
	}
	return cmp < 0
}

// Err reports the first stored field error seen while collecting.
func (c *TopHitsCalculator) Err() error { return c.err }

// Total is the number of documents in the bucket, not the number of hits kept.
func (c *TopHitsCalculator) Total() uint64 { return c.total }

// MaxScore is the best score of the kept hits, 0 when there is none.
func (c *TopHitsCalculator) MaxScore() float64 {
	var max float64
	for i := range c.hits {
		if c.hits[i].Score > max {
			max = c.hits[i].Score
		}
	}
	return max
}

// Hits returns the requested window of the ranked hits.
func (c *TopHitsCalculator) Hits() []TopHit {
	if c.from >= len(c.hits) {
		return nil
	}
	return c.hits[c.from:]
}
