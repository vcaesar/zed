/* Copyright 2022 Zinc Labs Inc. and Contributors
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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/index"
)

type objectInfo struct {
	Key  string
	Size int64
}

// objectStore is the subset of object storage operations the directory needs.
type objectStore interface {
	List(ctx context.Context, prefix string) ([]objectInfo, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, data []byte) error
	Delete(ctx context.Context, key string) error
}

// MemoryObjectStore is an in-memory objectStore used by tests, in this package
// and, through SetTestObjectStore, by packages that only see the exported API.
type MemoryObjectStore struct {
	mu   sync.Mutex
	objs map[string][]byte
	err  error
	gets int
}

func NewMemoryObjectStore() *MemoryObjectStore {
	return &MemoryObjectStore{objs: map[string][]byte{}}
}

// SetErr makes List and Get fail with err; nil clears the failure.
func (m *MemoryObjectStore) SetErr(err error) {
	m.mu.Lock()
	m.err = err
	m.mu.Unlock()
}

// Gets returns how many objects were fetched from the store.
func (m *MemoryObjectStore) Gets() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gets
}

// Len returns how many objects are stored under prefix.
func (m *MemoryObjectStore) Len(prefix string) int {
	objs, err := m.List(context.Background(), prefix)
	if err != nil {
		return 0
	}
	return len(objs)
}

func (m *MemoryObjectStore) List(_ context.Context, prefix string) ([]objectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	var rv []objectInfo
	for k, v := range m.objs {
		if strings.HasPrefix(k, prefix) {
			rv = append(rv, objectInfo{Key: k, Size: int64(len(v))})
		}
	}
	sort.Slice(rv, func(i, j int) bool { return rv[i].Key < rv[j].Key })
	return rv, nil
}

func (m *MemoryObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	m.gets++
	v, ok := m.objs[key]
	if !ok {
		return nil, fmt.Errorf("object not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(v)), nil
}

func (m *MemoryObjectStore) Put(_ context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = append([]byte(nil), data...)
	return nil
}

func (m *MemoryObjectStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
	return nil
}

// backend lazily builds the process-wide objectStore of one storage type.
type backend struct {
	newStore func() (objectStore, error)
	mu       sync.Mutex
	inited   bool
	store    objectStore
	err      error
}

func (b *backend) get() (objectStore, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.inited {
		b.store, b.err = b.newStore()
		b.inited = true
	}
	return b.store, b.err
}

// setStore replaces the store of an already or not yet initialized backend and
// returns a function restoring the previous state.
func (b *backend) setStore(store objectStore) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	oldInited, oldStore, oldErr := b.inited, b.store, b.err
	b.inited, b.store, b.err = true, store, nil
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.inited, b.store, b.err = oldInited, oldStore, oldErr
	}
}

// backendFor returns the backend of an object storage type.
func backendFor(storageType string) (*backend, error) {
	switch storageType {
	case "s3", "minio":
		return s3Backend, nil
	case "gcs":
		return gcsBackend, nil
	case "oss":
		return ossBackend, nil
	default:
		return nil, fmt.Errorf("storage type [%s] is not an object storage", storageType)
	}
}

// SetTestObjectStore installs store as the backend of storageType and returns a
// function restoring the previous one. It exists so packages outside directory
// can exercise the object storage paths without a real bucket.
func SetTestObjectStore(storageType string, store *MemoryObjectStore) (func(), error) {
	b, err := backendFor(storageType)
	if err != nil {
		return nil, err
	}
	return b.setStore(store), nil
}

// isIndexItem reports whether name is a bluge index item (segment or snapshot).
// Everything else found next to them (pid files, WAL) must not be copied.
func isIndexItem(name string) bool {
	ext := path.Ext(name)
	return ext == index.ItemKindSegment || ext == index.ItemKindSnapshot
}

// directoryMarker marks an object prefix as an existing directory, so a shard
// that holds no segments yet can still be opened read-only. Its name matches no
// item kind, so List and Load never see it.
const directoryMarker = ".zinc_directory"

// CopyDiskToObject uploads every index item of indexName found under rootPath to
// the object storage backend of storageType. Items are immutable and keyed by
// name, so re-running after a partial copy simply overwrites what it re-uploads.
func CopyDiskToObject(storageType, rootPath, indexName string) error {
	store, err := storeFor(storageType)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := store.Put(ctx, indexName+"/"+directoryMarker, nil); err != nil {
		return err
	}
	dir := filepath.Join(rootPath, filepath.FromSlash(indexName))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing was ever written for this shard
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !isIndexItem(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		if err := store.Put(ctx, indexName+"/"+entry.Name(), data); err != nil {
			return err
		}
	}
	return nil
}

// CopyObjectToDisk downloads every index item of indexName from the object
// storage backend of storageType into rootPath.
func CopyObjectToDisk(storageType, rootPath, indexName string) error {
	store, err := storeFor(storageType)
	if err != nil {
		return err
	}
	ctx := context.Background()
	objs, err := store.List(ctx, indexName+"/")
	if err != nil {
		return err
	}
	dir := filepath.Join(rootPath, filepath.FromSlash(indexName))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, obj := range objs {
		name := path.Base(obj.Key)
		if !isIndexItem(name) {
			continue
		}
		if err := copyObjectToFile(ctx, store, obj.Key, filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyObjectToFile(ctx context.Context, store objectStore, key, dst string) error {
	rc, err := store.Get(ctx, key)
	if err != nil {
		return err
	}
	defer func() {
		if err := rc.Close(); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("object store: close body")
		}
	}()
	f, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".*"+cacheTempExt)
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("path", tmp).Msg("object copy: temp cleanup failed")
		}
	}()
	_, copyErr := io.Copy(f, rc)
	if err := errors.Join(copyErr, f.Close()); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// ObjectIndexHasData reports whether indexName has any index item stored, as
// opposed to only the directory marker of a shard that was never written to.
func ObjectIndexHasData(storageType, indexName string) (bool, error) {
	store, err := storeFor(storageType)
	if err != nil {
		return false, err
	}
	objs, err := store.List(context.Background(), indexName+"/")
	if err != nil {
		return false, err
	}
	for _, obj := range objs {
		if isIndexItem(path.Base(obj.Key)) {
			return true, nil
		}
	}
	return false, nil
}

// RemoveObjectIndexByType deletes every object and cached file of indexName in
// the backend of storageType.
func RemoveObjectIndexByType(storageType, indexName string) error {
	b, err := backendFor(storageType)
	if err != nil {
		return err
	}
	return removeObjectIndex(b, indexName)
}

func storeFor(storageType string) (objectStore, error) {
	b, err := backendFor(storageType)
	if err != nil {
		return nil, err
	}
	return b.get()
}

// getObjectConfig returns a riot config that stores index items as objects
// under "<indexName>/" in the backend, cached locally by defaultFileCache.
func getObjectConfig(b *backend, indexName string, timeRange []int64) (riot.Config, error) {
	store, err := b.get()
	if err != nil {
		return riot.Config{}, err
	}
	cache, err := defaultFileCache()
	if err != nil {
		return riot.Config{}, fmt.Errorf("object cache: %w", err)
	}
	cfg := index.DefaultConfigWithDirectory(func() index.Directory {
		return NewObjectDirectory(store, cache, indexName)
	})
	cfg = cfg.WithPersisterNapTimeMSec(50)
	if len(timeRange) == 2 {
		if timeRange[0] <= timeRange[1] {
			cfg = cfg.WithTimeRange(timeRange[0], timeRange[1])
		}
	}
	return riot.DefaultConfigWithIndexConfig(cfg), nil
}

// removeObjectIndex deletes every object and cached file stored for the index.
func removeObjectIndex(b *backend, indexName string) error {
	store, err := b.get()
	if err != nil {
		return err
	}
	cache, err := defaultFileCache()
	if err != nil {
		return fmt.Errorf("object cache: %w", err)
	}
	return removeObjectPrefix(store, cache, indexName)
}

func removeObjectPrefix(store objectStore, cache *fileCache, prefix string) error {
	ctx := context.Background()
	if cache != nil {
		if err := cache.RemovePrefix(prefix + "/"); err != nil {
			return err
		}
	}
	objs, err := store.List(ctx, prefix+"/")
	if err != nil {
		return err
	}
	for _, obj := range objs {
		if err := store.Delete(ctx, obj.Key); err != nil {
			return err
		}
	}
	return nil
}

// ObjectDirectory implements index.Directory on top of an objectStore.
// With a cache, items are kept as local files and memory-mapped; without one
// they are loaded fully into memory on every open.
type ObjectDirectory struct {
	store  objectStore
	cache  *fileCache // nil disables local caching
	prefix string
}

func NewObjectDirectory(store objectStore, cache *fileCache, indexName string) *ObjectDirectory {
	return &ObjectDirectory{store: store, cache: cache, prefix: indexName + "/"}
}

func (d *ObjectDirectory) key(kind string, id uint64) string {
	return d.prefix + fmt.Sprintf("%012x", id) + kind
}

func (d *ObjectDirectory) Setup(readOnly bool) error {
	if !readOnly {
		return nil
	}
	objs, err := d.store.List(context.Background(), d.prefix)
	if err != nil {
		return err
	}
	if len(objs) == 0 {
		return fmt.Errorf("readOnly, directory does not exist")
	}
	return nil
}

func (d *ObjectDirectory) List(kind string) ([]uint64, error) {
	objs, err := d.store.List(context.Background(), d.prefix)
	if err != nil {
		return nil, err
	}
	var rv uint64Slice
	for _, obj := range objs {
		name := path.Base(obj.Key)
		if !strings.HasSuffix(name, kind) {
			continue
		}
		base := name[:len(name)-len(kind)]
		id, err := strconv.ParseUint(base, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing identifier '%s': %w", base, err)
		}
		rv = append(rv, id)
	}
	sort.Sort(sort.Reverse(rv))
	return rv, nil
}

func (d *ObjectDirectory) Load(kind string, id uint64) (*segment.Data, io.Closer, error) {
	key := d.key(kind, id)
	if d.cache != nil {
		if data, closer, ok := d.cache.Get(key); ok {
			return data, closer, nil
		}
		if err := d.fetchToCache(key); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("object cache: falling back to in-memory load")
		} else if data, closer, ok := d.cache.Get(key); ok {
			return data, closer, nil
		}
	}
	rc, err := d.store.Get(context.Background(), key)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err := rc.Close(); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("object store: close body")
		}
	}()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, err
	}
	return segment.NewDataBytes(data), io.NopCloser(nil), nil
}

func (d *ObjectDirectory) fetchToCache(key string) error {
	rc, err := d.store.Get(context.Background(), key)
	if err != nil {
		return err
	}
	defer func() {
		if err := rc.Close(); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("object store: close body")
		}
	}()
	return d.cache.Put(key, rc)
}

func (d *ObjectDirectory) Persist(kind string, id uint64, w index.WriterTo, closeCh chan struct{}) error {
	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf, closeCh); err != nil {
		return err
	}
	key := d.key(kind, id)
	if err := d.store.Put(context.Background(), key, buf.Bytes()); err != nil {
		return err
	}
	if d.cache != nil {
		if err := d.cache.Put(key, bytes.NewReader(buf.Bytes())); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("object cache: persist not cached")
		}
	}
	return nil
}

func (d *ObjectDirectory) Remove(kind string, id uint64) error {
	key := d.key(kind, id)
	if d.cache != nil {
		if err := d.cache.Remove(key); err != nil {
			return err
		}
	}
	return d.store.Delete(context.Background(), key)
}

func (d *ObjectDirectory) Stats() (numItems uint64, numBytes uint64) {
	objs, err := d.store.List(context.Background(), d.prefix)
	if err != nil {
		log.Error().Err(err).Str("prefix", d.prefix).Msg("object directory stats")
		return 0, 0
	}
	for _, obj := range objs {
		numItems++
		numBytes += uint64(obj.Size)
	}
	return numItems, numBytes
}

func (d *ObjectDirectory) Sync() error   { return nil }
func (d *ObjectDirectory) Lock() error   { return nil }
func (d *ObjectDirectory) Unlock() error { return nil }

type uint64Slice []uint64

func (e uint64Slice) Len() int           { return len(e) }
func (e uint64Slice) Swap(i, j int)      { e[i], e[j] = e[j], e[i] }
func (e uint64Slice) Less(i, j int) bool { return e[i] < e[j] }
