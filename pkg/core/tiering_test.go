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

	"github.com/vcaesar/zedsearch/pkg/bluge/directory"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/metadata"
)

// useObjectStore points the s3 backend at an in-memory store and makes it the
// cold storage type for the duration of the test.
func useObjectStore(t *testing.T) *directory.MemoryObjectStore {
	t.Helper()
	store := directory.NewMemoryObjectStore()
	restore, err := directory.SetTestObjectStore(StorageTypeS3, store)
	require.NoError(t, err)

	oldBucket := config.Global.S3.Bucket
	oldCold := config.Global.Tier.ColdStorageType
	config.Global.S3.Bucket = "zinc-test"
	config.Global.Tier.ColdStorageType = StorageTypeS3
	t.Cleanup(func() {
		config.Global.S3.Bucket = oldBucket
		config.Global.Tier.ColdStorageType = oldCold
		restore()
	})
	return store
}

// countDocs searches every shard of the index and returns the document count.
func countDocs(index *Index) (uint64, error) {
	readers, err := index.GetReaders(0, 0)
	if err != nil {
		return 0, err
	}
	var total uint64
	for _, r := range readers {
		n, err := r.Count()
		if err != nil {
			_ = r.Close()
			return 0, err
		}
		total += n
		if err := r.Close(); err != nil {
			return 0, err
		}
	}
	return total, nil
}

// newTierIndex creates a hot index holding one document.
func newTierIndex(t *testing.T, name string) *Index {
	t.Helper()
	index, err := NewIndex(name, StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(index))
	require.NoError(t, index.CreateDocument("1", map[string]interface{}{"name": "tiered document"}, false))
	require.Eventually(t, func() bool {
		n, err := countDocs(index)
		return err == nil && n == 1
	}, 30*time.Second, 50*time.Millisecond, "document was not applied from the WAL")
	return index
}

func localShardDirsExist(index *Index) bool {
	for _, name := range index.shardDataNames() {
		if _, err := os.Stat(filepath.Join(config.Global.DataPath, filepath.FromSlash(name))); err == nil {
			return true
		}
	}
	return false
}

func TestMoveIndexTier(t *testing.T) {
	store := useObjectStore(t)
	name := "TestMoveIndexTier.index_1"
	index := newTierIndex(t, name)
	t.Cleanup(func() { _ = DeleteIndex(name) })

	t.Run("invalid tier", func(t *testing.T) {
		assert.Error(t, MoveIndexTier(name, "lukewarm"))
		assert.Error(t, MoveIndexTier("TestMoveIndexTier.index_missing", TierCold))
	})

	t.Run("hot to cold", func(t *testing.T) {
		require.NoError(t, MoveIndexTier(name, TierCold))
		assert.Equal(t, TierCold, index.GetTier())
		assert.Equal(t, StorageTypeS3, index.GetStorageType())
		assert.True(t, index.IsReadOnly())
		assert.Positive(t, store.Len(name+"/"), "segments were copied to object storage")
		assert.False(t, localShardDirsExist(index), "local copy is dropped after a successful move")
	})

	t.Run("cold is searchable", func(t *testing.T) {
		n, err := countDocs(index)
		assert.NoError(t, err)
		assert.Equal(t, uint64(1), n)
	})

	t.Run("cold rejects writes", func(t *testing.T) {
		doc := map[string]interface{}{"name": "nope"}
		assert.ErrorIs(t, index.CreateDocument("2", doc, false), ErrIndexReadOnly)
		assert.ErrorIs(t, index.UpdateDocument("1", doc, false), ErrIndexReadOnly)
		assert.ErrorIs(t, index.DeleteDocument("1"), ErrIndexReadOnly)
		_, err := anyShard(index).GetWriter()
		assert.ErrorIs(t, err, ErrIndexReadOnly)
	})

	t.Run("second move is a no-op", func(t *testing.T) {
		objects := store.Len(name + "/")
		require.NoError(t, MoveIndexTier(name, TierCold))
		assert.Equal(t, objects, store.Len(name+"/"))
		n, err := countDocs(index)
		assert.NoError(t, err)
		assert.Equal(t, uint64(1), n)
	})

	t.Run("re-heat to hot", func(t *testing.T) {
		require.NoError(t, MoveIndexTier(name, TierHot))
		assert.Equal(t, TierHot, index.GetTier())
		assert.Equal(t, StorageTypeDisk, index.GetStorageType())
		assert.False(t, index.IsReadOnly())
		assert.Equal(t, 0, store.Len(name+"/"), "object copy is dropped after re-heating")
		assert.True(t, localShardDirsExist(index))

		n, err := countDocs(index)
		assert.NoError(t, err)
		assert.Equal(t, uint64(1), n)
		assert.NoError(t, index.CreateDocument("2", map[string]interface{}{"name": "warm again"}, false))
	})
}

// anyShard returns one first layer shard of the index.
func anyShard(index *Index) *IndexShard {
	for _, shard := range index.shards {
		return shard
	}
	return nil
}

func TestMoveIndexTierUnconfigured(t *testing.T) {
	oldBucket := config.Global.S3.Bucket
	oldCold := config.Global.Tier.ColdStorageType
	config.Global.S3.Bucket = ""
	config.Global.Tier.ColdStorageType = StorageTypeS3
	defer func() {
		config.Global.S3.Bucket = oldBucket
		config.Global.Tier.ColdStorageType = oldCold
	}()

	name := "TestMoveIndexTierUnconfigured.index_1"
	index, err := NewIndex(name, StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(index))
	defer func() { _ = DeleteIndex(name) }()

	assert.Error(t, MoveIndexTier(name, TierCold))
	assert.Equal(t, TierHot, index.GetTier())
	assert.False(t, index.IsReadOnly())
}

func TestTierStorageType(t *testing.T) {
	oldCold := config.Global.Tier.ColdStorageType
	defer func() { config.Global.Tier.ColdStorageType = oldCold }()

	got, err := tierStorageType(TierHot)
	assert.NoError(t, err)
	assert.Equal(t, StorageTypeDisk, got)

	config.Global.Tier.ColdStorageType = StorageTypeDisk
	_, err = tierStorageType(TierCold)
	assert.Error(t, err)
}

func TestCheckTier(t *testing.T) {
	for _, tier := range []string{TierHot, TierWarm, TierCold} {
		assert.NoError(t, CheckTier(tier))
	}
	assert.Error(t, CheckTier(""))
	assert.Error(t, CheckTier("frozen"))
}

func TestParseAge(t *testing.T) {
	for _, tt := range []struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		{value: "7d", want: 7 * 24 * time.Hour},
		{value: "0.5d", want: 12 * time.Hour},
		{value: "90m", want: 90 * time.Minute},
		{value: " 2h ", want: 2 * time.Hour},
		{value: "", wantErr: true},
		{value: "xd", wantErr: true},
		{value: "7y", wantErr: true},
		{value: "-5d", wantErr: true},
		{value: "-1h", wantErr: true},
		{value: "infd", wantErr: true},
		{value: "NaNd", wantErr: true},
		{value: "1e300d", wantErr: true},
	} {
		t.Run(tt.value, func(t *testing.T) {
			got, err := ParseAge(tt.value)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLifecycleTick(t *testing.T) {
	useObjectStore(t)
	name := "TestLifecycleTick.index_1"
	index := newTierIndex(t, name)
	defer func() { _ = DeleteIndex(name) }()

	index.lock.Lock()
	index.ref.Settings.Lifecycle = &meta.IndexLifecycle{
		WarmAfter:   "7d",
		ColdAfter:   "30d",
		DeleteAfter: "90d",
	}
	index.lock.Unlock()

	base := time.Now()
	index.ref.Stats.DocTimeMax = base.UnixNano()
	day := 24 * time.Hour

	t.Run("young index stays hot", func(t *testing.T) {
		runTieringOnce(base.Add(day))
		assert.Equal(t, TierHot, index.GetTier())
	})

	t.Run("warm after 7d", func(t *testing.T) {
		runTieringOnce(base.Add(8 * day))
		assert.Equal(t, TierWarm, index.GetTier())
		assert.Equal(t, StorageTypeS3, index.GetStorageType())
		assert.False(t, index.IsReadOnly(), "warm stays writable")
	})

	t.Run("cold after 30d", func(t *testing.T) {
		runTieringOnce(base.Add(31 * day))
		assert.Equal(t, TierCold, index.GetTier())
		assert.True(t, index.IsReadOnly())
	})

	t.Run("delete after 90d", func(t *testing.T) {
		runTieringOnce(base.Add(91 * day))
		_, exists := GetIndex(name)
		assert.False(t, exists)
	})
}

// TestIndexAgeFromShards checks the age comes from the shards that hold the
// documents: the index level stats are not maintained by the WAL consumer.
func TestIndexAgeFromShards(t *testing.T) {
	name := "TestIndexAgeFromShards.index_1"
	index := newTierIndex(t, name)
	defer func() { _ = DeleteIndex(name) }()

	index.lock.RLock()
	indexStat := index.ref.Stats.DocTimeMax
	index.lock.RUnlock()
	assert.Zero(t, indexStat, "index level doc_time_max stays empty")

	age, ok := indexAge(index, time.Now().Add(48*time.Hour))
	require.True(t, ok, "an index holding a document has an age")
	assert.Greater(t, age, 47*time.Hour)
	assert.Less(t, age, 49*time.Hour)
}

// TestIndexAgeDrivesLifecycle is the same for the mover: an index that was
// written to must be deleted once it is older than delete_after.
func TestIndexAgeDrivesLifecycle(t *testing.T) {
	name := "TestIndexAgeDrivesLifecycle.index_1"
	index := newTierIndex(t, name)
	defer func() { _ = DeleteIndex(name) }()

	index.lock.Lock()
	index.ref.Settings.Lifecycle = &meta.IndexLifecycle{DeleteAfter: "1d"}
	index.lock.Unlock()

	runTieringOnce(time.Now().Add(2 * 24 * time.Hour))
	_, exists := GetIndex(name)
	assert.False(t, exists, "an expired index is deleted")
}

// TestGetTierConcurrentWithMove hammers the readers of the index settings and
// stats while a move stores a new tier: everything must go through the index
// lock, a plain read of ref.Settings.Tier races with setLocation.
func TestGetTierConcurrentWithMove(t *testing.T) {
	name := "TestGetTierConcurrentWithMove.index_1"
	index, err := NewIndex(name, StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(index))
	defer func() { _ = DeleteIndex(name) }()

	index.lock.Lock()
	index.ref.Settings.Lifecycle = &meta.IndexLifecycle{WarmAfter: "7d"}
	index.lock.Unlock()

	var readers sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = index.GetTier()
				_ = index.IsCold()
				lifecycle, err := ResolveLifecycle(index)
				if err == nil && lifecycle != nil {
					_, _, _ = ExplainTiering(index, lifecycle, time.Now())
				}
			}
		}()
	}
	for i := 0; i < 25; i++ {
		require.NoError(t, index.setLocation(StorageTypeDisk, TierWarm))
		require.NoError(t, index.setLocation(StorageTypeDisk, TierHot))
	}
	close(stop)
	readers.Wait()
	assert.Equal(t, TierHot, index.GetTier())
}

func TestLifecycleWithoutDocuments(t *testing.T) {
	name := "TestLifecycleWithoutDocuments.index_1"
	index, err := NewIndex(name, StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(index))
	defer func() { _ = DeleteIndex(name) }()

	index.lock.Lock()
	index.ref.Settings.Lifecycle = &meta.IndexLifecycle{DeleteAfter: "1d"}
	index.lock.Unlock()

	runTieringOnce(time.Now().Add(365 * 24 * time.Hour))
	_, exists := GetIndex(name)
	assert.True(t, exists, "an index without documents has no age and is never moved")
}

func TestResolveLifecycle(t *testing.T) {
	name := "TestResolveLifecycle.index_1"
	index, err := NewIndex(name, StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(index))
	defer func() { _ = DeleteIndex(name) }()

	t.Run("no lifecycle", func(t *testing.T) {
		got, err := ResolveLifecycle(index)
		assert.NoError(t, err)
		assert.Nil(t, got)
	})

	policy := "TestResolveLifecycle.policy"
	require.NoError(t, metadata.ILM.Set(policy, metadata.ILMPolicyRecord{
		Policy: metadata.ILMPolicy{Phases: map[string]metadata.ILMPhase{
			"warm":   {MinAge: "7d"},
			"cold":   {MinAge: "30d"},
			"delete": {MinAge: "90d"},
		}},
	}))
	defer func() { _ = metadata.ILM.Delete(policy) }()

	t.Run("from policy", func(t *testing.T) {
		index.lock.Lock()
		index.ref.Settings.Lifecycle = &meta.IndexLifecycle{Policy: policy}
		index.lock.Unlock()

		got, err := ResolveLifecycle(index)
		assert.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "7d", got.WarmAfter)
		assert.Equal(t, "30d", got.ColdAfter)
		assert.Equal(t, "90d", got.DeleteAfter)
	})

	t.Run("index overrides policy", func(t *testing.T) {
		index.lock.Lock()
		index.ref.Settings.Lifecycle = &meta.IndexLifecycle{Policy: policy, WarmAfter: "1d"}
		index.lock.Unlock()

		got, err := ResolveLifecycle(index)
		assert.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "1d", got.WarmAfter)
		assert.Equal(t, "30d", got.ColdAfter)
	})

	t.Run("missing policy", func(t *testing.T) {
		index.lock.Lock()
		index.ref.Settings.Lifecycle = &meta.IndexLifecycle{Policy: "TestResolveLifecycle.missing"}
		index.lock.Unlock()

		_, err := ResolveLifecycle(index)
		assert.Error(t, err)

		index.lock.Lock()
		index.ref.Settings.Lifecycle = nil
		index.lock.Unlock()
	})
}

func TestExplainTiering(t *testing.T) {
	name := "TestExplainTiering.index_1"
	index, err := NewIndex(name, StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(index))
	defer func() { _ = DeleteIndex(name) }()

	now := time.Now()
	lifecycle := &meta.IndexLifecycle{WarmAfter: "7d", ColdAfter: "30d", DeleteAfter: "90d"}

	t.Run("no documents", func(t *testing.T) {
		age, action, next := ExplainTiering(index, lifecycle, now)
		assert.Zero(t, age)
		assert.Empty(t, action)
		assert.Empty(t, next)
	})

	index.ref.Stats.DocTimeMax = now.Add(-24 * time.Hour).UnixNano()

	t.Run("not due yet reports the next phase", func(t *testing.T) {
		age, action, next := ExplainTiering(index, lifecycle, now)
		assert.InDelta(t, 24*time.Hour, age, float64(time.Minute))
		assert.Equal(t, ILMPhaseWarm, action)
		assert.Equal(t, TierWarm, next)
	})

	t.Run("due", func(t *testing.T) {
		index.ref.Stats.DocTimeMax = now.Add(-31 * 24 * time.Hour).UnixNano()
		_, action, next := ExplainTiering(index, lifecycle, now)
		assert.Equal(t, ILMPhaseCold, action)
		assert.Equal(t, TierCold, next)
	})

	t.Run("unmanaged", func(t *testing.T) {
		_, action, next := ExplainTiering(index, nil, now)
		assert.Empty(t, action)
		assert.Empty(t, next)
	})
}

func TestStartTieringDisabled(t *testing.T) {
	old := config.Global.Tier.Enable
	config.Global.Tier.Enable = false
	defer func() { config.Global.Tier.Enable = old }()

	// returns immediately and starts no goroutine
	StartTiering(t.Context())
}
