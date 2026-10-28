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

package core

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/metadata"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

func waitDocs(t *testing.T, index *Index, want uint64) {
	t.Helper()
	require.Eventually(t, func() bool {
		n, err := countDocs(index)
		return err == nil && n == want
	}, 30*time.Second, 50*time.Millisecond)
}

func TestIdempotentEntry(t *testing.T) {
	tests := []struct {
		name       string
		action     string
		wantAction string
	}{
		{name: "insert becomes update", action: meta.ActionTypeInsert, wantAction: meta.ActionTypeUpdate},
		{name: "update stays", action: meta.ActionTypeUpdate, wantAction: meta.ActionTypeUpdate},
		{name: "delete stays", action: meta.ActionTypeDelete, wantAction: meta.ActionTypeDelete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, err := json.Marshal(map[string]interface{}{
				meta.IDFieldName: "1", meta.ActionFieldName: tt.action, meta.ShardFieldName: 3,
			})
			require.NoError(t, err)
			out, err := idempotentEntry(entry)
			require.NoError(t, err)
			doc := map[string]interface{}{}
			require.NoError(t, json.Unmarshal(out, &doc))
			assert.Equal(t, tt.wantAction, doc[meta.ActionFieldName])
			assert.InDelta(t, float64(ShardIDNeedUpdate), doc[meta.ShardFieldName], 0)
		})
	}
	_, err := idempotentEntry([]byte("{"))
	assert.Error(t, err)
}

// WriteGuard rejects a write before the WAL; replica entries bypass it.
// DropLocalIndex removes the local copy but keeps the shared metadata.
func TestWriteGuardAndDropLocalIndex(t *testing.T) {
	name := "TestWriteGuardAndDropLocalIndex.index_1"
	index, err := NewIndex(name, StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(index))
	ZED_INDEX_LIST.Add(index)
	t.Cleanup(func() {
		ZED_INDEX_LIST.Delete(name)
		assert.NoError(t, metadata.Index.Delete(name))
	})

	WriteGuard = func(string) error { return assert.AnError }
	t.Cleanup(func() { WriteGuard = nil })
	assert.ErrorIs(t, index.CreateDocument("1", map[string]interface{}{"name": "one"}, false), assert.AnError)
	shardID := ""
	for id := range index.shards {
		shardID = id
	}
	entry := []byte(`{"@_id":"1","@_action":"insert","@_shard":-1,"name":"one"}`)
	require.NoError(t, ApplyReplicaEntries(index, shardID, [][]byte{entry}))
	waitDocs(t, index, 1)
	WriteGuard = nil

	require.NoError(t, DropLocalIndex(name))
	_, ok := GetIndex(name)
	assert.False(t, ok)
	_, err = os.Stat(filepath.Join(config.Global.DataPath, name))
	assert.True(t, os.IsNotExist(err))
	require.NoError(t, DropLocalIndex(name), "dropping a missing index is a no-op")
	_, err = metadata.Index.Get(name)
	assert.NoError(t, err, "the shared metadata stays")
	assert.Error(t, index.CreateDocument("2", map[string]interface{}{}, false), "a dropped copy stays closed")
}

// The hook records owner writes; entries replayed twice on a replica stay
// one document.
func TestWALHookAndApplyReplicaEntries(t *testing.T) {
	var mu sync.Mutex
	recorded := map[string][][]byte{}
	WALHook = func(index, shard string, entry []byte) {
		mu.Lock()
		recorded[shard] = append(recorded[shard], append([]byte(nil), entry...))
		mu.Unlock()
	}
	t.Cleanup(func() { WALHook = nil })

	owner, err := NewIndex("TestWALHookAndApplyReplicaEntries.owner", StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(owner))
	require.NoError(t, owner.CreateDocument("1", map[string]interface{}{"name": "first"}, false))
	require.NoError(t, owner.CreateDocument("2", map[string]interface{}{"name": "second"}, false))
	waitDocs(t, owner, 2)
	require.NoError(t, owner.DeleteDocument("2"))
	WALHook = nil

	mu.Lock()
	require.Len(t, recorded, 1)
	mu.Unlock()

	replica, err := NewIndex("TestWALHookAndApplyReplicaEntries.replica", StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(replica))
	for shardID := range replica.shards {
		for _, entries := range recorded {
			require.NoError(t, ApplyReplicaEntries(replica, shardID, entries))
			require.NoError(t, ApplyReplicaEntries(replica, shardID, entries[:1]))
		}
	}
	waitDocs(t, replica, 1)

	assert.Error(t, ApplyReplicaEntries(replica, "missing", nil))
}

func TestSnapshotInstall(t *testing.T) {
	source := newTierIndex(t, "TestSnapshotInstall.index_1")
	require.NoError(t, source.CreateDocument("2", map[string]interface{}{"name": "second"}, false))

	dir := t.TempDir()
	staged := filepath.Join(config.Global.DataPath, "_replication_test", "TestSnapshotInstall")
	require.NoError(t, os.MkdirAll(filepath.Dir(staged), 0o750))
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(filepath.Dir(staged))) })

	indexMeta, err := source.SnapshotTo(dir)
	require.NoError(t, err)
	require.NoError(t, os.Rename(dir, staged))

	// install over a renamed copy of the metadata, as a replica would
	stored := new(meta.Index)
	require.NoError(t, json.Unmarshal(indexMeta, stored))
	stored.Name = "TestSnapshotInstall.index_2"
	renamed, err := json.Marshal(stored)
	require.NoError(t, err)

	old, err := NewIndex("TestSnapshotInstall.index_2", StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(old))
	require.NoError(t, InstallSnapshot("TestSnapshotInstall.index_2", renamed, staged))

	installed, ok := GetIndex("TestSnapshotInstall.index_2")
	require.True(t, ok)
	assert.NotSame(t, old, installed)
	assert.ErrorIs(t, old.CreateDocument("x", map[string]interface{}{}, false), ErrIndexReadOnly, "the replaced copy is retired")
	waitDocs(t, installed, 2)

	assert.Error(t, InstallSnapshot("other", renamed, staged), "name mismatch")
	assert.Error(t, InstallSnapshot("other", []byte("{"), staged))
}

func TestSnapshotToRejectsObjectStorage(t *testing.T) {
	useObjectStore(t)
	index, err := NewIndex("TestSnapshotToRejectsObjectStorage.index_1", StorageTypeS3, 1)
	require.NoError(t, err)
	_, err = index.SnapshotTo(t.TempDir())
	assert.Error(t, err)
	assert.False(t, index.IsDisk())
}

func TestFlushAndLoadIndex(t *testing.T) {
	index := newTierIndex(t, "TestFlushAndLoadIndex.index_1")
	require.NoError(t, index.Flush())
	// writers and WAL reopen on the next write
	require.NoError(t, index.CreateDocument("2", map[string]interface{}{"name": "after flush"}, false))
	waitDocs(t, index, 2)

	loaded, err := LoadIndex("TestFlushAndLoadIndex.index_1")
	require.NoError(t, err)
	assert.Same(t, index, loaded)

	// an index only the metadata store knows is loaded from it
	ZED_INDEX_LIST.Delete("TestFlushAndLoadIndex.index_1")
	loaded, err = LoadIndex("TestFlushAndLoadIndex.index_1")
	require.NoError(t, err)
	assert.Equal(t, "TestFlushAndLoadIndex.index_1", loaded.GetName())

	_, err = LoadIndex("TestFlushAndLoadIndex.missing")
	assert.Error(t, err)
}

func TestStoreGuard(t *testing.T) {
	StoreGuard = func(string) bool { return false }
	t.Cleanup(func() { StoreGuard = nil })
	index, err := NewIndex("TestStoreGuard.index_1", StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(index))
	StoreGuard = nil
	ZED_INDEX_LIST.Delete("TestStoreGuard.index_1")
	_, err = LoadIndex("TestStoreGuard.index_1")
	assert.Error(t, err, "a guarded index must not reach the metadata store")
}
