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

package logs

import (
	"fmt"
	"maps"
	"math"
	"time"

	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils"
)

// Normalize returns a shallow copy with canonical UTC event time. The caller's
// fields, including message/log and the original _timestamp alias, are preserved.
// The date format/time_zone of the mapping for the field actually used applies.
func Normalize(doc map[string]interface{}, mappings *meta.Mappings, now time.Time) (map[string]interface{}, time.Time, error) {
	if doc == nil {
		return nil, time.Time{}, fmt.Errorf("log document must be an object")
	}
	field := meta.TimeFieldName
	value := doc[field]
	if value == nil {
		field = aliasFieldName
		value = doc[field]
	}
	timestamp := now
	if value != nil {
		// JSON numbers must be finite integers before conversion to int64.
		if n, ok := value.(float64); ok {
			if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n {
				return nil, time.Time{}, fmt.Errorf("field [%s] must be an integer timestamp", field)
			}
			if n < math.MinInt64 || n >= -float64(math.MinInt64) {
				return nil, time.Time{}, fmt.Errorf("field [%s] is outside the supported nanosecond timestamp range", field)
			}
		}
		var prop meta.Property
		if mappings != nil {
			prop, _ = mappings.GetProperty(field)
		}
		var err error
		timestamp, err = zutils.ParseTime(value, prop.Format, prop.TimeZone)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("field [%s] parse error: %w", field, err)
		}
	}
	if !time.Unix(0, timestamp.UnixNano()).Equal(timestamp) {
		return nil, time.Time{}, fmt.Errorf("field [%s] is outside the supported nanosecond timestamp range", field)
	}
	timestamp = timestamp.UTC()
	normalized := maps.Clone(doc)
	normalized[meta.TimeFieldName] = timestamp.Format(time.RFC3339Nano)
	return normalized, timestamp, nil
}
