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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vcaesar/riot"
)

func TestDisMaxQuery(t *testing.T) {
	mappings := testMappings()

	tests := []struct {
		name        string
		query       map[string]interface{}
		wantShoulds int
		wantErr     string
	}{
		{
			name:        "two queries",
			query:       map[string]interface{}{"queries": []interface{}{termOn("tag", "a"), termOn("tag", "b")}},
			wantShoulds: 2,
		},
		{
			name: "tie_breaker is accepted",
			query: map[string]interface{}{
				"QUERIES":     []interface{}{termOn("tag", "a"), termOn("tag", "b")},
				"TIE_BREAKER": 0.3,
			},
			wantShoulds: 2,
		},
		{
			name:    "empty queries",
			query:   map[string]interface{}{"queries": []interface{}{}},
			wantErr: "[dis_max] queries must be a non-empty array",
		},
		{
			name:    "missing queries",
			query:   map[string]interface{}{"tie_breaker": 0.3},
			wantErr: "[dis_max] queries must be a non-empty array",
		},
		{
			name:    "queries not an array",
			query:   map[string]interface{}{"queries": termOn("tag", "a")},
			wantErr: "[dis_max] queries must be a non-empty array",
		},
		{
			name:    "query item not a map",
			query:   map[string]interface{}{"queries": []interface{}{"a"}},
			wantErr: "[dis_max] queries doesn't support values of type: string",
		},
		{
			name:    "bad tie_breaker",
			query:   map[string]interface{}{"queries": []interface{}{termOn("tag", "a")}, "tie_breaker": "low"},
			wantErr: "[dis_max] tie_breaker doesn't support values of type: string",
		},
		{
			name:    "bad boost",
			query:   map[string]interface{}{"queries": []interface{}{termOn("tag", "a")}, "boost": "high"},
			wantErr: "[dis_max] boost doesn't support values of type: string",
		},
		{
			name:    "unknown field",
			query:   map[string]interface{}{"queries": []interface{}{termOn("tag", "a")}, "nope": 1},
			wantErr: "[dis_max] unknown field [nope]",
		},
		{
			name:    "nested parse error",
			query:   map[string]interface{}{"queries": []interface{}{map[string]interface{}{"nope": map[string]interface{}{}}}},
			wantErr: "[dis_max] failed to parse field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := DisMaxQuery(tt.query, mappings, nil)
			if tt.wantErr != "" {
				assert.Nil(t, q)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
			bq, ok := q.(*riot.BooleanQuery)
			require.True(t, ok)
			assert.Len(t, bq.Shoulds(), tt.wantShoulds)
			assert.Empty(t, bq.Musts())
			assert.Empty(t, bq.MustNots())
			assert.Equal(t, 1, bq.MinShould())
		})
	}
}

// TestDisMaxQueryBoost pins that the boost every elasticsearch query accepts
// is applied instead of being rejected as an unknown field.
func TestDisMaxQueryBoost(t *testing.T) {
	q, err := DisMaxQuery(map[string]interface{}{
		"queries": []interface{}{termOn("tag", "a"), termOn("tag", "b")},
		"boost":   2.5,
	}, testMappings(), nil)
	require.NoError(t, err)

	bq, ok := q.(*riot.BooleanQuery)
	require.True(t, ok)
	assert.Equal(t, 2.5, bq.Boost())
	assert.Len(t, bq.Shoulds(), 2)
}
