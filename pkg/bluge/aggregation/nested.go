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

package aggregation

import (
	"github.com/vcaesar/riot/search"
)

// NewNestedAggregation returns the aggregation backing [nested]: a single
// bucket over the hidden documents that carry pathField == path.
//
// Nested documents are hidden from root searches, so this aggregation only
// sees documents when the query itself selects them (for example a
// [nested] query). Otherwise the bucket is empty.
func NewNestedAggregation(pathField, path string) *FilterAggregation {
	return NewFilterAggregation(&FilterPredicate{
		Fields: []string{pathField},
		Match: func(d *search.DocumentMatch) bool {
			for _, v := range search.Field(pathField).Values(d) {
				if string(v) == path {
					return true
				}
			}
			return false
		},
	})
}

// NewReverseNestedAggregation returns the aggregation backing
// [reverse_nested]: a single bucket over the root documents, i.e. those
// without a nested path.
func NewReverseNestedAggregation(pathField string) *FilterAggregation {
	return NewFilterAggregation(&FilterPredicate{
		Fields: []string{pathField},
		Match: func(d *search.DocumentMatch) bool {
			return len(search.Field(pathField).Values(d)) == 0
		},
	})
}
