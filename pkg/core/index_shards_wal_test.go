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

func Test_walMergeDocs_InOtherShard(t *testing.T) {
	entry := func(id string, shardID int64) map[string]interface{} {
		return map[string]interface{}{
			meta.IDFieldName:     id,
			meta.ActionFieldName: meta.ActionTypeUpdate,
			meta.ShardFieldName:  float64(shardID),
		}
	}
	docs := make(walMergeDocs)
	docs.AddDocument(entry("1", 0))

	tests := []struct {
		name string
		data map[string]interface{}
		want bool
	}{
		{"same shard", entry("1", 0), false},
		{"other shard", entry("1", ShardIDNeedUpdate), true},
		{"other document", entry("2", ShardIDNeedUpdate), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, docs.InOtherShard(tt.data))
		})
	}

	docs.Reset()
	assert.False(t, docs.InOtherShard(entry("1", ShardIDNeedUpdate)))
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

	// map order of the merged shards is random, repeat to catch a reorder
	for i := 0; i < 20; i++ {
		require.NoError(t, index.DeleteDocument("1"))
		require.NoError(t, index.CreateDocument("1", doc, true))
		require.True(t, shard.ConsumeWAL())
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
