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
	"github.com/vcaesar/riot/search"
)

// MissingAggregation counts the matching documents that have no value for the
// field, as the elasticsearch missing aggregation does.
type MissingAggregation struct {
	src search.FieldSource
}

// NewMissingAggregation returns the aggregation backing [missing].
func NewMissingAggregation(src search.FieldSource) *MissingAggregation {
	return &MissingAggregation{src: src}
}

func (a *MissingAggregation) Fields() []string {
	return a.src.Fields()
}

func (a *MissingAggregation) Calculator() search.Calculator {
	return &MissingCalculator{src: a.src}
}

// MissingCalculator holds the doc count. It deliberately does not implement
// search.MetricCalculator so the response layer can render {"doc_count": n}.
type MissingCalculator struct {
	src   search.FieldSource
	count uint64
}

// Consume counts a document when it carries no doc value for the field.
// FieldSource.Values reads DocumentMatch.DocValues, which also materializes
// numeric column fields, so this works for text and numeric fields alike.
func (c *MissingCalculator) Consume(d *search.DocumentMatch) {
	if len(c.src.Values(d)) == 0 {
		c.count++
	}
}

func (c *MissingCalculator) Merge(other search.Calculator) {
	if o, ok := other.(*MissingCalculator); ok {
		c.count += o.count
	}
}

func (c *MissingCalculator) Finish() {}

func (c *MissingCalculator) DocCount() uint64 { return c.count }
