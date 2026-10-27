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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vcaesar/riot"
)

func TestConstantScoreQuery(t *testing.T) {
	mappings := testMappings()

	tests := []struct {
		name      string
		query     map[string]interface{}
		wantBoost float64
		wantErr   string
	}{
		{
			name:      "default boost",
			query:     map[string]interface{}{"filter": termOn("tag", "a")},
			wantBoost: 1.0,
		},
		{
			name:      "explicit boost",
			query:     map[string]interface{}{"FILTER": termOn("tag", "a"), "BOOST": 2.5},
			wantBoost: 2.5,
		},
		{
			name:      "boost as string",
			query:     map[string]interface{}{"filter": termOn("tag", "a"), "boost": "3"},
			wantBoost: 3.0,
		},
		{
			name:    "missing filter",
			query:   map[string]interface{}{"boost": 2.0},
			wantErr: "[constant_score] requires a [filter] query",
		},
		{
			name:    "non-map filter",
			query:   map[string]interface{}{"filter": []interface{}{termOn("tag", "a")}},
			wantErr: "[constant_score] filter doesn't support values of type: []interface {}",
		},
		{
			name:    "bad boost",
			query:   map[string]interface{}{"filter": termOn("tag", "a"), "boost": "high"},
			wantErr: "[constant_score] boost doesn't support values of type: string",
		},
		{
			name:    "unknown field",
			query:   map[string]interface{}{"filter": termOn("tag", "a"), "nope": 1},
			wantErr: "[constant_score] unknown field [nope]",
		},
		{
			name:    "nested parse error",
			query:   map[string]interface{}{"filter": map[string]interface{}{"nope": map[string]interface{}{}}},
			wantErr: "[constant_score] failed to parse field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := ConstantScoreQuery(tt.query, mappings, nil)
			if tt.wantErr != "" {
				assert.Nil(t, q)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
			csq, ok := q.(*constantScoreQuery)
			require.True(t, ok)
			assert.Equal(t, tt.wantBoost, csq.Boost())
			assert.IsType(t, &riot.TermQuery{}, csq.Query())
			assert.NoError(t, csq.Validate())
		})
	}
}

// TestConstantScoreQuery_Search indexes a few documents and verifies that every
// hit of a constant_score query is scored with the configured boost.
func TestConstantScoreQuery_Search(t *testing.T) {
	writer, err := riot.OpenWriter(riot.InMemoryOnlyConfig())
	require.NoError(t, err)
	defer func() { assert.NoError(t, writer.Close()) }()

	batch := riot.NewBatch()
	for id, name := range map[string]string{
		"1": "red apple",
		"2": "green apple pie",
		"3": "blue sky",
	} {
		batch.Update(riot.Identifier(id), riot.NewDocument(id).AddField(riot.NewTextField("name", name)))
	}
	require.NoError(t, writer.Batch(batch))

	reader, err := writer.Reader()
	require.NoError(t, err)
	defer func() { assert.NoError(t, reader.Close()) }()

	const boost = 2.5
	q, err := ConstantScoreQuery(map[string]interface{}{
		"filter": map[string]interface{}{"match": map[string]interface{}{"name": "apple"}},
		"boost":  boost,
	}, testMappings(), nil)
	require.NoError(t, err)

	dmi, err := reader.Search(context.Background(), riot.NewTopNSearch(10, q))
	require.NoError(t, err)

	hits := 0
	match, err := dmi.Next()
	for err == nil && match != nil {
		hits++
		assert.Equal(t, boost, match.Score)
		match, err = dmi.Next()
	}
	require.NoError(t, err)
	assert.Equal(t, 2, hits)
}
