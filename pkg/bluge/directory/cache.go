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
	"container/list"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/mmap-go"
	"github.com/rs/zerolog/log"
	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/zincsearch/zincsearch/pkg/config"
)

const cacheTempExt = ".tmp"

// fileCache is a size-bounded LRU of immutable index items kept on local disk
// so object-store backed indexes are not re-downloaded on every open.
// Keys are object keys; files live at <root>/<key>.
type fileCache struct {
	root    string
	maxSize int64

	mu    sync.Mutex
	size  int64
	items map[string]*list.Element // key -> *cacheItem
	lru   *list.List               // front = least recently used
}

type cacheItem struct {
	key  string
	size int64
	refs int // open readers; never evicted while > 0
}

var (
	cacheOnce sync.Once
	cacheInst *fileCache
	cacheErr  error
)

// defaultFileCache returns the shared cache, or nil when ZINC_OBJECT_CACHE_SIZE is 0.
func defaultFileCache() (*fileCache, error) {
	cacheOnce.Do(func() {
		if config.Global.ObjectCacheSize <= 0 {
			return
		}
		cacheInst, cacheErr = newFileCache(path.Join(config.Global.DataPath, "objcache"), config.Global.ObjectCacheSize)
	})
	return cacheInst, cacheErr
}

// newFileCache opens root, drops leftover temp files and registers existing
// files oldest first so the cache survives restarts.
func newFileCache(root string, maxSize int64) (*fileCache, error) {
	// root is cleaned so path() can tell keys inside it from keys escaping it
	root = filepath.Clean(root)
	c := &fileCache{root: root, maxSize: maxSize, items: map[string]*list.Element{}, lru: list.New()}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	type found struct {
		key  string
		size int64
		mod  time.Time
	}
	var files []found
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasSuffix(p, cacheTempExt) {
			return os.Remove(p)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files = append(files, found{key: filepath.ToSlash(rel), size: info.Size(), mod: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, f := range files {
		c.add(f.key, f.size)
	}
	c.evict()
	return c, nil
}

// path returns the file of key. Keys are built from index names, which may
// contain "..", so a key that would escape the cache root is rejected instead
// of reading, writing or deleting somewhere else on disk.
func (c *fileCache) path(key string) (string, error) {
	p := filepath.Join(c.root, filepath.FromSlash(key))
	if p != c.root && !strings.HasPrefix(p, c.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("object cache: key [%s] escapes the cache directory", key)
	}
	return p, nil
}

// removeFile deletes the file of key; a file that is already gone is not an error.
func (c *fileCache) removeFile(key string) error {
	p, err := c.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Get memory-maps the cached file for key. ok is false on a miss.
// The closer must be called once the data is no longer used.
func (c *fileCache) Get(key string) (*segment.Data, io.Closer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, nil, false
	}
	it := el.Value.(*cacheItem)
	p, err := c.path(key)
	if err != nil {
		c.drop(el)
		log.Warn().Err(err).Str("key", key).Msg("object cache: get")
		return nil, nil, false
	}
	f, err := os.Open(p)
	if err != nil {
		c.drop(el)
		return nil, nil, false
	}
	data, unmap, err := mmapFile(f)
	if err != nil {
		log.Warn().Err(errors.Join(err, f.Close())).Str("key", key).Msg("object cache: mmap failed")
		return nil, nil, false
	}
	c.lru.MoveToBack(el)
	it.refs++
	return data, closerFunc(func() error {
		c.mu.Lock()
		it.refs--
		c.mu.Unlock()
		return unmap.Close()
	}), true
}

// Put stores r under key. Items are immutable, so an already cached key is left untouched.
func (c *fileCache) Put(key string, r io.Reader) error {
	p, err := c.path(key)
	if err != nil {
		return err
	}
	c.mu.Lock()
	_, exists := c.items[key]
	c.mu.Unlock()
	if exists {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".*"+cacheTempExt)
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("key", key).Msg("object cache: temp cleanup failed")
		}
	}()
	n, copyErr := io.Copy(f, r)
	if err := errors.Join(copyErr, f.Close()); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[key]; exists { // concurrent Put of the same key
		return nil
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	c.add(key, n)
	c.evict()
	return nil
}

// Remove forgets key and deletes its file.
func (c *fileCache) Remove(key string) error {
	if _, err := c.path(key); err != nil {
		return err
	}
	c.mu.Lock()
	if el, ok := c.items[key]; ok {
		c.drop(el)
	}
	c.mu.Unlock()
	return c.removeFile(key)
}

// RemovePrefix forgets every key under prefix and deletes the directory.
func (c *fileCache) RemovePrefix(prefix string) error {
	p, err := c.path(prefix)
	if err != nil {
		return err
	}
	c.mu.Lock()
	for key, el := range c.items {
		if strings.HasPrefix(key, prefix) {
			c.drop(el)
		}
	}
	c.mu.Unlock()
	return os.RemoveAll(p)
}

// Size returns bytes currently accounted for.
func (c *fileCache) Size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.size
}

// add, drop and evict must be called with mu held.
func (c *fileCache) add(key string, size int64) {
	c.items[key] = c.lru.PushBack(&cacheItem{key: key, size: size})
	c.size += size
}

func (c *fileCache) drop(el *list.Element) {
	it := el.Value.(*cacheItem)
	delete(c.items, it.key)
	c.lru.Remove(el)
	c.size -= it.size
}

func (c *fileCache) evict() {
	for el := c.lru.Front(); el != nil && c.size > c.maxSize; {
		next := el.Next()
		if it := el.Value.(*cacheItem); it.refs == 0 {
			c.drop(el)
			if err := c.removeFile(it.key); err != nil {
				log.Warn().Err(err).Str("key", it.key).Msg("object cache: evict failed")
			}
		}
		el = next
	}
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func mmapFile(f *os.File) (*segment.Data, io.Closer, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if info.Size() == 0 {
		return segment.NewDataBytes([]byte{}), f, nil
	}
	mm, err := mmap.Map(f, mmap.RDONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	return segment.NewDataBytes(mm), closerFunc(func() error {
		err := mm.Unmap()
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}), nil
}
