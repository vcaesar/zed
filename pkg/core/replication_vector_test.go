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
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vcaesar/riot"
	blugeindex "github.com/vcaesar/riot/index"

	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

var testANN = riot.ANNParams{M: 4, EfConstruction: 16}

func countGraphs(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(path) == blugeindex.ItemKindGraph {
			n++
		}
		return err
	}))
	return n
}

// annSearch runs an approximate knn search on every reader of idx and
// returns the matched ids.
func annSearch(t *testing.T, idx *Index) []string {
	t.Helper()
	readers, err := idx.GetReaders(0, 0)
	require.NoError(t, err)
	var ids []string
	for _, r := range readers {
		q := riot.NewKNNQuery("emb", []float32{1, 0.1, 0}, 3).SetMetric(riot.L2).SetANN(testANN)
		it, err := r.Search(context.Background(), riot.NewTopNSearch(10, q))
		require.NoError(t, err)
		for m, err := it.Next(); m != nil; m, err = it.Next() {
			require.NoError(t, err)
			require.NoError(t, m.VisitStoredFields(func(field string, value []byte) bool {
				if field == "_id" {
					ids = append(ids, string(value))
				}
				return true
			}))
		}
		require.NoError(t, r.Close())
	}
	return ids
}

// A snapshot carries the persisted HNSW graphs of its segments, so a replica
// installing it searches without rebuilding them.
func TestSnapshotCarriesVectorGraphs(t *testing.T) {
	name := "TestSnapshotCarriesVectorGraphs.index_1"
	replicaName := "TestSnapshotCarriesVectorGraphs.index_2"
	source, err := NewIndex(name, StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, StoreIndex(source))
	prop := meta.NewProperty("vector")
	prop.Dims = 3
	prop.IndexOptions = &meta.VectorIndexOptions{M: testANN.M, EfConstruction: testANN.EfConstruction}
	source.GetMappings().SetProperty("emb", prop)
	ZED_INDEX_LIST.Add(source)
	t.Cleanup(func() { assert.NoError(t, DeleteIndex(name)) })

	const docs = 50
	for i := range docs {
		v := float64(i)
		require.NoError(t, source.CreateDocument(strconv.Itoa(i), map[string]interface{}{"emb": []interface{}{v, v / 10, 0}}, false))
	}
	waitDocs(t, source, docs)
	require.NoError(t, source.Flush()) // persist the segments

	live := filepath.Join(config.Global.DataPath, name)
	assert.ElementsMatch(t, []string{"0", "1", "2"}, annSearch(t, source))
	require.Eventually(t, func() bool { return countGraphs(t, live) > 0 },
		10*time.Second, 20*time.Millisecond, "the owner persists the graphs it built")

	staged := filepath.Join(config.Global.DataPath, "_replication_test", "TestSnapshotCarriesVectorGraphs")
	require.NoError(t, os.RemoveAll(staged))
	require.NoError(t, os.MkdirAll(filepath.Dir(staged), 0o750))
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(filepath.Dir(staged))) })

	indexMeta, err := source.SnapshotTo(staged)
	require.NoError(t, err)
	graphs := countGraphs(t, live)
	assert.Equal(t, graphs, countGraphs(t, staged), "the snapshot carries every graph")

	// install it as a replica would and search the copy
	stored := new(meta.Index)
	require.NoError(t, json.Unmarshal(indexMeta, stored))
	stored.Name = replicaName
	renamed, err := json.Marshal(stored)
	require.NoError(t, err)
	require.NoError(t, InstallSnapshot(replicaName, renamed, staged))
	t.Cleanup(func() { assert.NoError(t, DropLocalIndex(replicaName)) })
	replica, ok := GetIndex(replicaName)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"0", "1", "2"}, annSearch(t, replica))
	assert.Equal(t, graphs, countGraphs(t, filepath.Join(config.Global.DataPath, replicaName)),
		"the replica keeps the shipped graphs")
}

func TestLinkGraphs(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	for _, file := range []string{"000000010001.hnsw", "000000000001.seg", "000000000001.snp"} {
		require.NoError(t, os.WriteFile(filepath.Join(src, file), []byte(file), 0o640))
	}
	require.NoError(t, linkGraphs(src, dst))
	entries, err := os.ReadDir(dst)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only graphs are linked")
	assert.Equal(t, "000000010001.hnsw", entries[0].Name())

	assert.Error(t, linkGraphs(src, dst), "an existing target is an error")
	require.NoError(t, linkGraphs(filepath.Join(src, "missing"), dst), "no source graphs")
}
