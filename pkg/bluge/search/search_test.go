/* Copyright 2026 AtomAI. and Contributors
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

package search

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vcaesar/riot"

	"github.com/vcaesar/zedsearch/pkg/meta"
)

// shardReaders returns one in-memory reader per shard, shard s holding docs
// "s-i" with numeric field n = s*perShard+i.
func shardReaders(t testing.TB, shards, perShard int) []*riot.Reader {
	t.Helper()
	readers := make([]*riot.Reader, 0, shards)
	for s := range shards {
		w, err := riot.OpenWriter(riot.InMemoryOnlyConfig())
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, w.Close()) })

		batch := riot.NewBatch()
		for i := range perShard {
			id := fmt.Sprintf("%d-%d", s, i)
			doc := riot.NewDocument(id).AddField(riot.NewNumericField("n", float64(s*perShard+i)).Sortable())
			batch.Update(doc.ID(), doc)
		}
		require.NoError(t, w.Batch(batch))

		r, err := w.Reader()
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, r.Close()) })
		readers = append(readers, r)
	}
	return readers
}

func TestMultiSearch(t *testing.T) {
	readers := shardReaders(t, 3, 5) // n = 0..14
	mappings := meta.NewMappings()
	mappings.SetProperty("n", meta.NewProperty("numeric"))

	tests := []struct {
		name      string
		from      int
		size      int
		readers   []*riot.Reader
		wantIDs   []string
		wantTotal int64
	}{
		{"top", 0, 4, readers, []string{"2-4", "2-3", "2-2", "2-1"}, 15},
		{"from crosses shards", 4, 3, readers, []string{"2-0", "1-4", "1-3"}, 15},
		{"from past end", 20, 5, readers, nil, 15},
		{"size larger than hits", 12, 10, readers, []string{"0-2", "0-1", "0-0"}, 15},
		{"single reader", 1, 2, readers[:1], []string{"0-3", "0-2"}, 5},
		{"no reader", 0, 5, nil, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := &meta.ZedQuery{
				Query: map[string]interface{}{"match_all": map[string]interface{}{}},
				Sort:  "-n",
				From:  tt.from,
				Size:  tt.size,
			}
			dmi, err := MultiSearch(context.Background(), query, mappings, nil, tt.readers...)
			require.NoError(t, err)

			var ids []string
			next, err := dmi.Next()
			for err == nil && next != nil {
				require.NoError(t, next.VisitStoredFields(func(field string, value []byte) bool {
					if field == "_id" {
						ids = append(ids, string(value))
					}
					return true
				}))
				next, err = dmi.Next()
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantIDs, ids)
			if tt.readers != nil {
				assert.Equal(t, uint64(tt.wantTotal), dmi.Aggregations().Count())
			}
		})
	}
}

func BenchmarkMultiSearch(b *testing.B) {
	readers := shardReaders(b, 3, 20)
	mappings := meta.NewMappings()
	mappings.SetProperty("n", meta.NewProperty("numeric"))
	b.ReportAllocs()
	for b.Loop() {
		query := &meta.ZedQuery{Query: map[string]interface{}{"match_all": map[string]interface{}{}}, Size: 10}
		dmi, err := MultiSearch(context.Background(), query, mappings, nil, readers...)
		if err != nil {
			b.Fatal(err)
		}
		for next, err := dmi.Next(); next != nil; next, err = dmi.Next() {
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}
