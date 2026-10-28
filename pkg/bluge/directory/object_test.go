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

package directory

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/index"
)

type bytesWriterTo []byte

func (b bytesWriterTo) WriteTo(w io.Writer, _ chan struct{}) (int64, error) {
	n, err := w.Write(b)
	return int64(n), err
}

func readData(t *testing.T, data *segment.Data, closer io.Closer) string {
	t.Helper()
	b, err := data.Read(0, data.Len())
	assert.NoError(t, err)
	s := string(b)
	assert.NoError(t, closer.Close())
	return s
}

func TestBackendFor(t *testing.T) {
	for _, tt := range []struct {
		storageType string
		want        *backend
	}{
		{"s3", s3Backend},
		{"minio", s3Backend},
		{"gcs", gcsBackend},
		{"oss", ossBackend},
		{"disk", nil},
		{"unknown", nil},
	} {
		t.Run(tt.storageType, func(t *testing.T) {
			got, err := backendFor(tt.storageType)
			if tt.want == nil {
				assert.Error(t, err)
				assert.Nil(t, got)
			} else {
				assert.NoError(t, err)
				assert.Same(t, tt.want, got)
			}
		})
	}
}

func TestObjectDirectory(t *testing.T) {
	for _, cached := range []bool{false, true} {
		name := "no cache"
		var cache *fileCache
		if cached {
			name = "cache"
			var err error
			cache, err = newFileCache(t.TempDir(), 1<<20)
			assert.NoError(t, err)
		}
		t.Run(name, func(t *testing.T) {
			store := NewMemoryObjectStore()
			d := NewObjectDirectory(store, cache, "idx/shard/000000")

			t.Run("setup", func(t *testing.T) {
				assert.Error(t, d.Setup(true), "read-only on an empty directory must fail")
				assert.NoError(t, d.Setup(false))
			})

			t.Run("persist load list", func(t *testing.T) {
				assert.NoError(t, d.Persist(index.ItemKindSegment, 1, bytesWriterTo("one"), nil))
				assert.NoError(t, d.Persist(index.ItemKindSegment, 2, bytesWriterTo("two"), nil))
				assert.NoError(t, d.Persist(index.ItemKindSnapshot, 3, bytesWriterTo("snap"), nil))
				assert.NoError(t, d.Setup(true))

				ids, err := d.List(index.ItemKindSegment)
				assert.NoError(t, err)
				assert.Equal(t, []uint64{2, 1}, ids, "ids are returned descending")
				ids, err = d.List(index.ItemKindSnapshot)
				assert.NoError(t, err)
				assert.Equal(t, []uint64{3}, ids)

				data, closer, err := d.Load(index.ItemKindSegment, 2)
				assert.NoError(t, err)
				assert.Equal(t, "two", readData(t, data, closer))

				_, _, err = d.Load(index.ItemKindSegment, 99)
				assert.Error(t, err)

				items, bytesN := d.Stats()
				assert.Equal(t, uint64(3), items)
				assert.Equal(t, uint64(len("one")+len("two")+len("snap")), bytesN)
			})

			t.Run("prefix isolation", func(t *testing.T) {
				other := NewObjectDirectory(store, cache, "idx/shard/0000001")
				assert.NoError(t, other.Persist(index.ItemKindSegment, 7, bytesWriterTo("x"), nil))
				ids, err := d.List(index.ItemKindSegment)
				assert.NoError(t, err)
				assert.Equal(t, []uint64{2, 1}, ids)
			})

			t.Run("remove", func(t *testing.T) {
				assert.NoError(t, d.Remove(index.ItemKindSegment, 1))
				ids, err := d.List(index.ItemKindSegment)
				assert.NoError(t, err)
				assert.Equal(t, []uint64{2}, ids)
				assert.NoError(t, d.Sync())
				assert.NoError(t, d.Lock())
				assert.NoError(t, d.Unlock())
			})

			t.Run("bad key", func(t *testing.T) {
				assert.NoError(t, store.Put(context.Background(), "idx/shard/000000/zz.seg", []byte("x")))
				_, err := d.List(index.ItemKindSegment)
				assert.Error(t, err)
				assert.NoError(t, store.Delete(context.Background(), "idx/shard/000000/zz.seg"))
			})

			t.Run("store error", func(t *testing.T) {
				store.SetErr(errors.New("boom"))
				defer store.SetErr(nil)
				_, err := d.List(index.ItemKindSegment)
				assert.Error(t, err)
				assert.Error(t, d.Setup(true))
				items, bytesN := d.Stats()
				assert.Equal(t, uint64(0), items)
				assert.Equal(t, uint64(0), bytesN)
			})

			t.Run("remove index", func(t *testing.T) {
				assert.NoError(t, removeObjectPrefix(store, cache, "idx/shard/000000"))
				objs, err := store.List(context.Background(), "idx/shard/000000/")
				assert.NoError(t, err)
				assert.Empty(t, objs)
				objs, err = store.List(context.Background(), "idx/shard/0000001/")
				assert.NoError(t, err)
				assert.Len(t, objs, 1, "sibling prefix is untouched")
				if cache != nil {
					_, err = os.Stat(filepath.Join(cache.root, "idx", "shard", "000000"))
					assert.True(t, os.IsNotExist(err))
					_, err = os.Stat(filepath.Join(cache.root, "idx", "shard", "0000001"))
					assert.NoError(t, err)
				}
			})
		})
	}
}

// TestCopyObjectToDiskRenameFails makes the final rename fail and checks the
// download leaves no temporary file behind.
func TestCopyObjectToDiskRenameFails(t *testing.T) {
	store := NewMemoryObjectStore()
	restore, err := SetTestObjectStore("s3", store)
	assert.NoError(t, err)
	defer restore()

	root := t.TempDir()
	name := "TestCopyObjectToDiskRenameFails.index_1/shard0/000000"
	assert.NoError(t, store.Put(context.Background(), name+"/000000000001.seg", []byte("seg")))

	// a directory where the segment file must land makes os.Rename fail
	dir := filepath.Join(root, filepath.FromSlash(name))
	assert.NoError(t, os.MkdirAll(filepath.Join(dir, "000000000001.seg", "sub"), 0o755))

	assert.Error(t, CopyObjectToDisk("s3", root, name))
	entries, err := os.ReadDir(dir)
	assert.NoError(t, err)
	assert.Len(t, entries, 1, "temporary file must be cleaned up")
}

func TestObjectDirectoryCache(t *testing.T) {
	cache, err := newFileCache(t.TempDir(), 1<<20)
	assert.NoError(t, err)
	store := NewMemoryObjectStore()
	d := NewObjectDirectory(store, cache, "idx")

	t.Run("persist warms the cache", func(t *testing.T) {
		assert.NoError(t, d.Persist(index.ItemKindSegment, 1, bytesWriterTo("one"), nil))
		data, closer, err := d.Load(index.ItemKindSegment, 1)
		assert.NoError(t, err)
		assert.Equal(t, "one", readData(t, data, closer))
		assert.Equal(t, 0, store.Gets(), "served from cache")
	})

	t.Run("load fetches once", func(t *testing.T) {
		assert.NoError(t, store.Put(context.Background(), "idx/000000000002.seg", []byte("two")))
		for i := 0; i < 3; i++ {
			data, closer, err := d.Load(index.ItemKindSegment, 2)
			assert.NoError(t, err)
			assert.Equal(t, "two", readData(t, data, closer))
		}
		assert.Equal(t, 1, store.Gets())
		assert.Equal(t, int64(6), cache.Size())
	})

	t.Run("remove drops cached file", func(t *testing.T) {
		assert.NoError(t, d.Remove(index.ItemKindSegment, 2))
		assert.Equal(t, int64(3), cache.Size())
		_, _, err := d.Load(index.ItemKindSegment, 2)
		assert.Error(t, err)
	})

	t.Run("cache failure falls back to memory", func(t *testing.T) {
		assert.NoError(t, os.WriteFile(filepath.Join(cache.root, "blocked"), []byte("x"), 0o644))
		bd := NewObjectDirectory(store, cache, "blocked")
		assert.NoError(t, bd.Persist(index.ItemKindSegment, 1, bytesWriterTo("mem"), nil))
		data, closer, err := bd.Load(index.ItemKindSegment, 1)
		assert.NoError(t, err)
		assert.Equal(t, "mem", readData(t, data, closer))
	})
}

// TestObjectDirectoryRiot writes and reads a real riot index through the directory.
func TestObjectDirectoryRiot(t *testing.T) {
	for _, cached := range []bool{false, true} {
		var cache *fileCache
		if cached {
			var err error
			cache, err = newFileCache(t.TempDir(), 1<<20)
			assert.NoError(t, err)
		}
		store := NewMemoryObjectStore()
		cfg := index.DefaultConfigWithDirectory(func() index.Directory {
			return NewObjectDirectory(store, cache, "riot/000000")
		})
		rcfg := riot.DefaultConfigWithIndexConfig(cfg)

		w, err := riot.OpenWriter(rcfg)
		assert.NoError(t, err)
		doc := riot.NewDocument("1").AddField(riot.NewTextField("name", "hello object store").StoreValue())
		assert.NoError(t, w.Update(doc.ID(), doc))
		assert.NoError(t, w.Close())

		objs, err := store.List(context.Background(), "riot/000000/")
		assert.NoError(t, err)
		assert.NotEmpty(t, objs)

		r, err := riot.OpenReader(rcfg)
		assert.NoError(t, err)
		it, err := r.Search(context.Background(), riot.NewTopNSearch(10, riot.NewMatchAllQuery()))
		assert.NoError(t, err)
		n := 0
		for m, err := it.Next(); m != nil && err == nil; m, err = it.Next() {
			n++
		}
		assert.Equal(t, 1, n)
		assert.NoError(t, r.Close())
		if cached {
			assert.Positive(t, cache.Size())
		}
	}
}

func TestSetTestObjectStore(t *testing.T) {
	_, err := SetTestObjectStore("disk", NewMemoryObjectStore())
	assert.Error(t, err)

	store := NewMemoryObjectStore()
	restore, err := SetTestObjectStore("s3", store)
	assert.NoError(t, err)
	got, err := storeFor("minio")
	assert.NoError(t, err)
	assert.Same(t, store, got)
	restore()
}

func TestIsIndexItem(t *testing.T) {
	assert.True(t, isIndexItem("000000000001.seg"))
	assert.True(t, isIndexItem("000000000001.snp"))
	assert.True(t, isIndexItem("0000000000010abc.hnsw"))
	assert.False(t, isIndexItem("bluge.pid"))
	assert.False(t, isIndexItem("wal"))
}

func TestObjectDirectoryLoadMissing(t *testing.T) {
	cache, err := newFileCache(t.TempDir(), 1<<20)
	assert.NoError(t, err)
	for name, c := range map[string]*fileCache{"no cache": nil, "cache": cache} {
		t.Run(name, func(t *testing.T) {
			store := NewMemoryObjectStore()
			_, _, err := NewObjectDirectory(store, c, "missing").Load(index.ItemKindGraph, 1)
			assert.ErrorIs(t, err, fs.ErrNotExist)

			store.SetErr(errors.New("boom"))
			defer store.SetErr(nil)
			_, _, err = NewObjectDirectory(store, c, "missing").Load(index.ItemKindGraph, 1)
			assert.Error(t, err)
			assert.NotErrorIs(t, err, fs.ErrNotExist)
		})
	}
}

func TestNotExist(t *testing.T) {
	err := notExist("k", minio.ErrorResponse{Code: minio.NoSuchKey})
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.Contains(t, err.Error(), "k")

	other := minio.ErrorResponse{Code: minio.AccessDenied}
	assert.Equal(t, error(other), notExist("k", other))
}

// TestHNSWGraphPersist builds an HNSW graph on first ANN search and checks it
// is persisted next to the segment and reused, read-only, after reopening.
func TestHNSWGraphPersist(t *testing.T) {
	store := NewMemoryObjectStore()
	disk := t.TempDir()
	countObjects := func(prefix string) func(t *testing.T) int {
		return func(t *testing.T) int {
			objs, err := store.List(context.Background(), prefix+"/")
			assert.NoError(t, err)
			n := 0
			for _, o := range objs {
				if path.Ext(o.Key) == index.ItemKindGraph {
					n++
				}
			}
			return n
		}
	}
	countFiles := func(t *testing.T) int {
		files, err := filepath.Glob(filepath.Join(disk, "*"+index.ItemKindGraph))
		assert.NoError(t, err)
		return len(files)
	}
	cache, err := newFileCache(t.TempDir(), 1<<20)
	assert.NoError(t, err)
	for _, tt := range []struct {
		name   string
		config func() index.Config
		graphs func(t *testing.T) int
	}{
		{"object", func() index.Config {
			return index.DefaultConfigWithDirectory(func() index.Directory { return NewObjectDirectory(store, nil, "graph") })
		}, countObjects("graph")},
		{"object cached", func() index.Config {
			return index.DefaultConfigWithDirectory(func() index.Directory { return NewObjectDirectory(store, cache, "graph_cached") })
		}, countObjects("graph_cached")},
		{"disk", func() index.Config { return index.DefaultConfig(disk) }, countFiles},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var asyncErrs []error
			var mu sync.Mutex
			open := func() riot.Config {
				cfg := tt.config()
				cfg.AsyncError = func(err error) {
					mu.Lock()
					defer mu.Unlock()
					asyncErrs = append(asyncErrs, err)
				}
				return riot.DefaultConfigWithIndexConfig(cfg)
			}
			search := func(t *testing.T, r *riot.Reader) []string {
				q := riot.NewKNNQuery("emb", []float32{1, 0.1, 0}, 2).SetANN(riot.ANNParams{M: 4, EfConstruction: 16})
				it, err := r.Search(context.Background(), riot.NewTopNSearch(10, q))
				assert.NoError(t, err)
				var ids []string
				for m, err := it.Next(); m != nil && err == nil; m, err = it.Next() {
					assert.NoError(t, m.VisitStoredFields(func(field string, value []byte) bool {
						if field == "_id" {
							ids = append(ids, string(value))
						}
						return true
					}))
				}
				return ids
			}

			w, err := riot.OpenWriter(open())
			assert.NoError(t, err)
			batch := riot.NewBatch()
			for id, v := range map[string][]float32{"x": {1, 0, 0}, "y": {0, 1, 0}, "xy": {1, 1, 0}} {
				f, err := riot.NewVectorField("emb", v)
				assert.NoError(t, err)
				doc := riot.NewDocument(id).AddField(f)
				batch.Update(doc.ID(), doc)
			}
			assert.NoError(t, w.Batch(batch))
			assert.NoError(t, w.Close())
			assert.Zero(t, tt.graphs(t))

			// segments loaded from the directory persist their graphs
			w, err = riot.OpenWriter(open())
			assert.NoError(t, err)
			r, err := w.Reader()
			assert.NoError(t, err)
			assert.Equal(t, []string{"x", "xy"}, search(t, r))
			assert.NoError(t, r.Close())
			assert.NoError(t, w.Close())
			assert.Equal(t, 1, tt.graphs(t))

			// a read-only reader loads the persisted graph instead of rebuilding
			r, err = riot.OpenReader(open())
			assert.NoError(t, err)
			assert.Equal(t, []string{"x", "xy"}, search(t, r))
			assert.NoError(t, r.Close())
			assert.Equal(t, 1, tt.graphs(t))
			assert.Empty(t, asyncErrs)
		})
	}
}

func TestCopyDiskObjectRoundTrip(t *testing.T) {
	store := NewMemoryObjectStore()
	restore, err := SetTestObjectStore("s3", store)
	assert.NoError(t, err)
	defer restore()

	root := t.TempDir()
	name := "TestCopyDiskObjectRoundTrip.index_1/shard0/000000"
	dir := filepath.Join(root, filepath.FromSlash(name))
	assert.NoError(t, os.MkdirAll(dir, 0o755))
	for file, body := range map[string]string{
		"000000000001.seg": "seg-one",
		"000000000002.snp": "snap",
		"bluge.pid":        "12345",
	} {
		assert.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644))
	}

	t.Run("upload", func(t *testing.T) {
		assert.NoError(t, CopyDiskToObject("s3", root, name))
		assert.Equal(t, 3, store.Len(name+"/"), "two index items plus the directory marker, no pid file")
		rc, err := store.Get(context.Background(), name+"/000000000001.seg")
		assert.NoError(t, err)
		b, err := io.ReadAll(rc)
		assert.NoError(t, err)
		assert.NoError(t, rc.Close())
		assert.Equal(t, "seg-one", string(b))
	})

	t.Run("idempotent upload", func(t *testing.T) {
		assert.NoError(t, CopyDiskToObject("s3", root, name))
		assert.Equal(t, 3, store.Len(name+"/"))
	})

	t.Run("a shard without segments is still marked", func(t *testing.T) {
		empty := name + "/does-not-exist"
		assert.NoError(t, CopyDiskToObject("s3", root, empty))
		assert.Equal(t, 1, store.Len(empty+"/"))
		assert.NoError(t, NewObjectDirectory(store, nil, empty).Setup(true))
		ids, err := NewObjectDirectory(store, nil, empty).List(index.ItemKindSegment)
		assert.NoError(t, err)
		assert.Empty(t, ids, "the marker is not an index item")
		assert.NoError(t, RemoveObjectIndexByType("s3", empty))
	})

	t.Run("download", func(t *testing.T) {
		back := t.TempDir()
		assert.NoError(t, CopyObjectToDisk("s3", back, name))
		b, err := os.ReadFile(filepath.Join(back, filepath.FromSlash(name), "000000000001.seg"))
		assert.NoError(t, err)
		assert.Equal(t, "seg-one", string(b))
		_, err = os.Stat(filepath.Join(back, filepath.FromSlash(name), "bluge.pid"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("remove", func(t *testing.T) {
		assert.NoError(t, RemoveObjectIndexByType("s3", name))
		assert.Equal(t, 0, store.Len(name+"/"))
	})

	t.Run("errors", func(t *testing.T) {
		assert.Error(t, CopyDiskToObject("disk", root, name))
		assert.Error(t, CopyObjectToDisk("disk", root, name))
		assert.Error(t, RemoveObjectIndexByType("disk", name))
		store.SetErr(errors.New("boom"))
		defer store.SetErr(nil)
		assert.Error(t, CopyObjectToDisk("s3", root, name))
	})
}
