/* Copyright 2022 Zinc Labs Inc. and Contributors
* Copyright 2026 AtomAI
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

package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	blugeindex "github.com/vcaesar/riot/index"

	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/meta"
)

func Test_walMergeDocs_WriteTo(t *testing.T) {
	testData := []map[string]interface{}{
		{
			meta.IDFieldName:     "1",
			meta.ActionFieldName: meta.ActionTypeInsert,
			meta.ShardFieldName:  float64(0),
			meta.TimeFieldName:   float64(time.Now().Unix()),
			"name":               "test",
			"age":                float64(10),
		}, {
			meta.IDFieldName:     "2",
			meta.ActionFieldName: meta.ActionTypeInsert,
			meta.ShardFieldName:  float64(0),
			meta.TimeFieldName:   float64(time.Now().Unix()),
			"name":               "test",
			"age":                float64(10),
		},
	}

	var index *Index
	var shard *IndexShard
	var err error
	docs := make(walMergeDocs)
	t.Run("prepare", func(t *testing.T) {
		for _, d := range testData {
			docs.AddDocument(d)
		}

		index, err = NewIndex("Test_walMergeDocs_WriteTo.index_1", "", 2)
		assert.NoError(t, err)
		assert.NotNil(t, index)

		shard = index.GetShardByDocID("1")
		assert.NotNil(t, shard)

		mappings := meta.NewMappings()
		mappings.SetProperty("name", meta.NewProperty("text"))
		mappings.SetProperty("age", meta.NewProperty("numeric"))
		err = index.SetMappings(mappings)
		assert.NoError(t, err)

		err = StoreIndex(index)
		assert.NoError(t, err)
	})

	t.Run("writeTo", func(t *testing.T) {
		batch := blugeindex.NewBatch()
		err := docs.WriteTo(shard, batch, false)
		assert.NoError(t, err)
	})

	t.Run("rollback", func(t *testing.T) {
		batch := blugeindex.NewBatch()
		err := docs.WriteTo(shard, batch, true)
		assert.NoError(t, err)
	})

	t.Run("Cleanup", func(t *testing.T) {
		err := DeleteIndex("Test_walMergeDocs_WriteTo.index_1")
		assert.NoError(t, err)
	})
}

func Test_walMergeDocs_AddDocument(t *testing.T) {
	entry := func(id, action string, shardID int64) map[string]interface{} {
		return map[string]interface{}{
			meta.IDFieldName:     id,
			meta.ActionFieldName: action,
			meta.ShardFieldName:  float64(shardID),
		}
	}
	const del, upd = meta.ActionTypeDelete, meta.ActionTypeUpdate

	tests := []struct {
		name    string
		entries []map[string]interface{}
		shard   int64
		actions []string
	}{
		{"same shard", []map[string]interface{}{entry("1", upd, 0), entry("1", upd, 0)}, 0, []string{upd, upd}},
		{"delete then update", []map[string]interface{}{entry("1", del, 0), entry("1", upd, ShardIDNeedUpdate)}, ShardIDNeedUpdate, []string{del, upd}},
		{"update then delete", []map[string]interface{}{entry("1", upd, ShardIDNeedUpdate), entry("1", del, 0)}, ShardIDNeedUpdate, []string{upd, del}},
		{"back to its shard", []map[string]interface{}{entry("1", del, 0), entry("1", upd, ShardIDNeedUpdate), entry("1", upd, 0)}, ShardIDNeedUpdate, []string{del, upd, upd}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := make(walMergeDocs)
			for _, e := range tt.entries {
				docs.AddDocument(e)
			}
			// one pending document under one shard, all actions in order
			for id, pending := range docs {
				if id != tt.shard {
					assert.Empty(t, pending, "shard %d", id)
				}
			}
			require.Contains(t, docs[tt.shard], "1")
			assert.Equal(t, tt.actions, docs[tt.shard]["1"].actions)
			assert.Equal(t, tt.entries[len(tt.entries)-1], docs[tt.shard]["1"].data)
		})
	}
}

// A delete names the second layer shard of the document while a following
// update names none: consumed in one batch, the update must still win.
func TestIndexShard_ConsumeWALKeepsOrderAcrossShards(t *testing.T) {
	name := "TestIndexShard_ConsumeWALKeepsOrderAcrossShards.index_1"
	index, err := NewIndex(name, "disk", 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(index))
	t.Cleanup(func() { _ = DeleteIndex(name) })

	shard := anyShard(index)
	require.NoError(t, shard.OpenWAL())
	// consume by hand only, let a background pass in flight finish; closing
	// the index needs the consumer back
	ZED_INDEX_SHARD_WAL_LIST.Remove(shard.GetShardName())
	t.Cleanup(func() { ZED_INDEX_SHARD_WAL_LIST.Add(shard) })
	time.Sleep(2 * config.Global.WalSyncInterval)

	doc := map[string]interface{}{"name": "test"}
	require.NoError(t, index.CreateDocument("1", doc, false))
	require.True(t, shard.ConsumeWAL())
	_, err = index.GetDocument("1")
	require.NoError(t, err)

	// map order of the merged shards is random, repeat to catch a reorder; a
	// reader must not see the document missing while the batch is applied
	for i := 0; i < 20; i++ {
		require.NoError(t, index.DeleteDocument("1"))
		require.NoError(t, index.CreateDocument("1", doc, true))
		stop, missing := make(chan struct{}), make(chan error, 1)
		go func() {
			defer close(missing)
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := index.GetDocument("1"); err != nil {
					missing <- err
					return
				}
			}
		}()
		consumed := shard.ConsumeWAL()
		close(stop)
		require.True(t, consumed)
		require.NoError(t, <-missing, "round %d", i)
		_, err = index.GetDocument("1")
		require.NoError(t, err, "round %d", i)
	}
}

// A write racing a tier move must fail cleanly once the shard's WAL is closed.
func TestIndexShard_writeWALClosed(t *testing.T) {
	name := "TestIndexShard_writeWALClosed.index_1"
	index, err := NewIndex(name, "disk", 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(index))
	t.Cleanup(func() { _ = DeleteIndex(name) })

	shard := anyShard(index)
	require.NoError(t, shard.OpenWAL())
	require.NoError(t, shard.writeWAL([]byte(`{}`)))
	require.NoError(t, index.Close())
	assert.ErrorIs(t, shard.writeWAL([]byte(`{}`)), ErrIndexReadOnly)
}
