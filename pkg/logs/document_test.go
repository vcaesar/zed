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

package logs

import (
	"maps"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch/pkg/meta"
)

func TestNormalize(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	instant := time.Date(2022, 1, 2, 3, 4, 5, 123456789, time.UTC)
	custom := meta.NewProperty("date")
	custom.Format, custom.TimeZone = "2006-01-02 15:04:05", "America/Los_Angeles"
	canonicalCustom := meta.NewMappings()
	require.NoError(t, ApplyDefaults(canonicalCustom))
	canonicalCustom.SetProperty(meta.TimeFieldName, custom)
	aliasCustom := meta.NewMappings()
	require.NoError(t, ApplyDefaults(aliasCustom))
	aliasCustom.SetProperty(aliasFieldName, custom)
	tests := []struct {
		name     string
		doc      map[string]interface{}
		mappings *meta.Mappings
		want     time.Time
	}{
		{name: "missing", doc: map[string]interface{}{}, want: now},
		{name: "null", doc: map[string]interface{}{"@timestamp": nil, "_timestamp": nil}, want: now},
		{name: "RFC3339 nanoseconds", doc: map[string]interface{}{"@timestamp": "2022-01-02T03:04:05.123456789Z"}, want: instant},
		{name: "offset", doc: map[string]interface{}{"@timestamp": "2022-01-02T05:04:05.123456789+02:00"}, want: instant},
		{name: "epoch seconds", doc: map[string]interface{}{"@timestamp": float64(instant.Unix())}, want: instant.Truncate(time.Second)},
		{name: "epoch milliseconds", doc: map[string]interface{}{"@timestamp": float64(instant.UnixMilli())}, want: instant.Truncate(time.Millisecond)},
		{name: "microsecond alias", doc: map[string]interface{}{"_timestamp": float64(instant.UnixMicro())}, want: instant.Truncate(time.Microsecond)},
		{name: "nanosecond integer", doc: map[string]interface{}{"_timestamp": instant.UnixNano()}, want: instant},
		{name: "string alias", doc: map[string]interface{}{"_timestamp": "2022-01-02T03:04:05.123456789Z"}, want: instant},
		{name: "null canonical uses alias", doc: map[string]interface{}{"@timestamp": nil, "_timestamp": instant.UnixMicro()}, want: instant.Truncate(time.Microsecond)},
		{name: "canonical wins", doc: map[string]interface{}{"@timestamp": instant.Format(time.RFC3339Nano), "_timestamp": "ignored"}, want: instant},
		{name: "custom format", doc: map[string]interface{}{"@timestamp": "2022-01-01 19:04:05"}, mappings: canonicalCustom, want: instant.Truncate(time.Second)},
		{name: "alias ignores canonical format", doc: map[string]interface{}{"_timestamp": instant.Format(time.RFC3339Nano)}, mappings: canonicalCustom, want: instant},
		{name: "alias uses own format", doc: map[string]interface{}{"_timestamp": "2022-01-01 19:04:05"}, mappings: aliasCustom, want: instant.Truncate(time.Second)},
		{name: "canonical ignores alias format", doc: map[string]interface{}{"@timestamp": instant.Format(time.RFC3339Nano)}, mappings: aliasCustom, want: instant},
		{name: "epoch zero", doc: map[string]interface{}{"@timestamp": 0}, want: time.Unix(0, 0).UTC()},
		{name: "negative seconds", doc: map[string]interface{}{"@timestamp": int64(-1)}, want: time.Unix(-1, 0).UTC()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.doc["message"] = "original message"
			tt.doc["log"] = "separate log"
			tt.doc["attributes"] = map[string]interface{}{"service": "api"}
			before := maps.Clone(tt.doc)
			doc, timestamp, err := Normalize(tt.doc, tt.mappings, now)
			require.NoError(t, err)
			assert.Equal(t, tt.want, timestamp)
			assert.Equal(t, tt.want.Format(time.RFC3339Nano), doc[meta.TimeFieldName])
			assert.Equal(t, before, tt.doc, "must not mutate input")
			for field, value := range before {
				if field != meta.TimeFieldName {
					assert.Equal(t, value, doc[field], field)
				}
			}
		})
	}
}

func TestNormalizeInvalid(t *testing.T) {
	mappings := meta.NewMappings()
	require.NoError(t, ApplyDefaults(mappings))
	for _, field := range []string{meta.TimeFieldName, "_timestamp"} {
		for _, value := range []interface{}{"", "not a date", "2022-01-01T00:00:00", "3000-01-01T00:00:00Z", true, []interface{}{}, map[string]interface{}{}, 1.5, math.NaN(), math.Inf(1), math.Inf(-1), float64(math.MaxInt64), -1e30} {
			doc, _, err := Normalize(map[string]interface{}{field: value}, mappings, time.Now())
			assert.ErrorContains(t, err, field, "%v", value)
			assert.Nil(t, doc)
		}
	}
	for _, value := range []interface{}{float64(math.MaxInt64), -1e30} {
		_, _, err := Normalize(map[string]interface{}{meta.TimeFieldName: value}, mappings, time.Now())
		assert.ErrorContains(t, err, "range", "%v", value)
	}
	_, _, err := Normalize(map[string]interface{}{meta.TimeFieldName: 1.5}, mappings, time.Now())
	assert.ErrorContains(t, err, "integer")
	doc, _, err := Normalize(nil, mappings, time.Now())
	assert.ErrorContains(t, err, "object")
	assert.Nil(t, doc)
	_, _, err = Normalize(map[string]interface{}{"@timestamp": "bad", "_timestamp": "2022-01-01T00:00:00Z"}, mappings, time.Now())
	assert.ErrorContains(t, err, meta.TimeFieldName, "invalid canonical timestamp must not silently fall back")
}
