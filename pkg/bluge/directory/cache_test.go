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
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
)

func readCached(t *testing.T, c *fileCache, key string) (string, bool) {
	t.Helper()
	data, closer, ok := c.Get(key)
	if !ok {
		return "", false
	}
	b, err := data.Read(0, data.Len())
	assert.NoError(t, err)
	s := string(b) // copy out before the mapping is released
	assert.NoError(t, closer.Close())
	return s, true
}

func TestFileCache(t *testing.T) {
	root := t.TempDir()
	c, err := newFileCache(root, 10)
	assert.NoError(t, err)

	t.Run("miss then hit", func(t *testing.T) {
		_, ok := readCached(t, c, "idx/1.seg")
		assert.False(t, ok)
		assert.NoError(t, c.Put("idx/1.seg", strings.NewReader("aaaa")))
		got, ok := readCached(t, c, "idx/1.seg")
		assert.True(t, ok)
		assert.Equal(t, "aaaa", got)
		assert.Equal(t, int64(4), c.Size())
		_, err := os.Stat(filepath.Join(root, "idx", "1.seg"))
		assert.NoError(t, err)
	})

	t.Run("put existing key is a no-op", func(t *testing.T) {
		assert.NoError(t, c.Put("idx/1.seg", strings.NewReader("zzzzzzzz")))
		got, _ := readCached(t, c, "idx/1.seg")
		assert.Equal(t, "aaaa", got)
		assert.Equal(t, int64(4), c.Size())
	})

	t.Run("evicts least recently used", func(t *testing.T) {
		assert.NoError(t, c.Put("idx/2.seg", strings.NewReader("bbbb")))
		_, ok := readCached(t, c, "idx/1.seg") // 1 is now most recent
		assert.True(t, ok)
		assert.NoError(t, c.Put("idx/3.seg", strings.NewReader("cccc"))) // 12 > 10: evict 2
		_, ok = readCached(t, c, "idx/2.seg")
		assert.False(t, ok)
		_, ok = readCached(t, c, "idx/1.seg")
		assert.True(t, ok)
		_, err := os.Stat(filepath.Join(root, "idx", "2.seg"))
		assert.True(t, os.IsNotExist(err))
		assert.Equal(t, int64(8), c.Size())
	})

	t.Run("referenced items are not evicted", func(t *testing.T) {
		data, closer, ok := c.Get("idx/1.seg")
		assert.True(t, ok)
		assert.NoError(t, c.Put("idx/4.seg", strings.NewReader("dddd"))) // evicts 3, keeps pinned 1
		_, ok = readCached(t, c, "idx/3.seg")
		assert.False(t, ok)
		b, err := data.Read(0, data.Len())
		assert.NoError(t, err)
		assert.Equal(t, "aaaa", string(b))
		assert.NoError(t, closer.Close())
		_, ok = readCached(t, c, "idx/1.seg")
		assert.True(t, ok)
	})

	t.Run("remove", func(t *testing.T) {
		assert.NoError(t, c.Remove("idx/1.seg"))
		assert.NoError(t, c.Remove("idx/1.seg"), "removing twice is fine")
		_, ok := readCached(t, c, "idx/1.seg")
		assert.False(t, ok)
		assert.Equal(t, int64(4), c.Size())
	})

	t.Run("file vanished behind cache", func(t *testing.T) {
		assert.NoError(t, os.Remove(filepath.Join(root, "idx", "4.seg")))
		_, ok := readCached(t, c, "idx/4.seg")
		assert.False(t, ok)
		assert.Equal(t, int64(0), c.Size())
	})

	t.Run("remove prefix", func(t *testing.T) {
		assert.NoError(t, c.Put("a/1.seg", strings.NewReader("x")))
		assert.NoError(t, c.Put("a/2.seg", strings.NewReader("y")))
		assert.NoError(t, c.Put("b/1.seg", strings.NewReader("z")))
		assert.NoError(t, c.RemovePrefix("a/"))
		_, ok := readCached(t, c, "a/1.seg")
		assert.False(t, ok)
		_, ok = readCached(t, c, "b/1.seg")
		assert.True(t, ok)
		_, err := os.Stat(filepath.Join(root, "a"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("empty file", func(t *testing.T) {
		assert.NoError(t, c.Put("b/empty.seg", bytes.NewReader(nil)))
		got, ok := readCached(t, c, "b/empty.seg")
		assert.True(t, ok)
		assert.Equal(t, "", got)
	})
}

func TestFileCacheReopen(t *testing.T) {
	root := t.TempDir()
	c, err := newFileCache(root, 100)
	assert.NoError(t, err)
	assert.NoError(t, c.Put("idx/old.seg", strings.NewReader("old")))
	old := filepath.Join(root, "idx", "old.seg")
	assert.NoError(t, os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)))
	assert.NoError(t, c.Put("idx/new.seg", strings.NewReader("new")))
	assert.NoError(t, os.WriteFile(filepath.Join(root, "idx", "junk.seg.tmp"), []byte("junk"), 0o644))

	// small limit on reopen evicts the oldest file and drops leftover temp files
	c2, err := newFileCache(root, 4)
	assert.NoError(t, err)
	_, ok := readCached(t, c2, "idx/old.seg")
	assert.False(t, ok)
	got, ok := readCached(t, c2, "idx/new.seg")
	assert.True(t, ok)
	assert.Equal(t, "new", got)
	_, err = os.Stat(filepath.Join(root, "idx", "junk.seg.tmp"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(old)
	assert.True(t, os.IsNotExist(err))
}

type blockedCacheReader struct {
	reader  io.Reader
	started chan struct{}
	release chan struct{}
}

func (r *blockedCacheReader) Read(p []byte) (int, error) {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	<-r.release
	return r.reader.Read(p)
}

func TestFileCacheConcurrentPut(t *testing.T) {
	for _, sameKey := range []bool{true, false} {
		name := "same key"
		secondKey := "idx/1.seg"
		if !sameKey {
			name = "different keys"
			secondKey = "idx/2.seg"
		}
		t.Run(name, func(t *testing.T) {
			c, err := newFileCache(t.TempDir(), 100)
			if !assert.NoError(t, err) {
				return
			}
			first := &blockedCacheReader{strings.NewReader("first"), make(chan struct{}), make(chan struct{})}
			second := &blockedCacheReader{strings.NewReader("later"), make(chan struct{}), make(chan struct{})}
			firstDone := make(chan error, 1)
			secondDone := make(chan error, 1)
			go func() { firstDone <- c.Put("idx/1.seg", first) }()
			<-first.started
			go func() { secondDone <- c.Put(secondKey, second) }()
			<-second.started

			// Both temporary files are open before either writer publishes.
			close(first.release)
			assert.NoError(t, <-firstDone)
			got, ok := readCached(t, c, "idx/1.seg")
			assert.True(t, ok)
			assert.Equal(t, "first", got)
			close(second.release)
			assert.NoError(t, <-secondDone)

			got, ok = readCached(t, c, "idx/1.seg")
			assert.True(t, ok)
			assert.Equal(t, "first", got, "published files must remain immutable")
			wantSize := int64(5)
			wantFiles := 1
			if !sameKey {
				got, ok = readCached(t, c, secondKey)
				assert.True(t, ok)
				assert.Equal(t, "later", got)
				wantSize = 10
				wantFiles = 2
			}
			assert.Equal(t, wantSize, c.Size())
			files, err := os.ReadDir(filepath.Join(c.root, "idx"))
			assert.NoError(t, err)
			assert.Len(t, files, wantFiles, "temporary files must be cleaned up")
		})
	}
}

func TestFileCachePutReadError(t *testing.T) {
	c, err := newFileCache(t.TempDir(), 100)
	if !assert.NoError(t, err) {
		return
	}
	readErr := errors.New("download interrupted")
	reader := io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(readErr))
	assert.ErrorIs(t, c.Put("idx/1.seg", reader), readErr)
	_, ok := readCached(t, c, "idx/1.seg")
	assert.False(t, ok, "partial downloads must not be published")
	assert.Zero(t, c.Size())
	files, err := os.ReadDir(filepath.Join(c.root, "idx"))
	assert.NoError(t, err)
	assert.Empty(t, files)
	assert.NoError(t, c.Put("idx/1.seg", strings.NewReader("retry")))
	got, ok := readCached(t, c, "idx/1.seg")
	assert.True(t, ok)
	assert.Equal(t, "retry", got)
}

// TestFileCacheKeyEscapesRoot guards the cache against keys built from index
// names such as "..": nothing may be written to, or deleted from, outside root.
func TestFileCacheKeyEscapesRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "objcache")
	c, err := newFileCache(root, 100)
	if !assert.NoError(t, err) {
		return
	}
	neighbour := filepath.Join(base, "keep.seg")
	assert.NoError(t, os.WriteFile(neighbour, []byte("keep"), 0o644))

	assert.Error(t, c.Put("../escape.seg", strings.NewReader("x")))
	_, err = os.Stat(filepath.Join(base, "escape.seg"))
	assert.True(t, os.IsNotExist(err), "put must not write outside the cache root")

	_, _, ok := c.Get("../keep.seg")
	assert.False(t, ok)
	assert.Error(t, c.Remove("../keep.seg"))
	assert.Error(t, c.RemovePrefix("../"))

	_, err = os.Stat(neighbour)
	assert.NoError(t, err, "files outside the cache root must survive")
	_, err = os.Stat(root)
	assert.NoError(t, err)
	assert.Zero(t, c.Size())
}

func TestFileCachePutError(t *testing.T) {
	root := t.TempDir()
	c, err := newFileCache(root, 100)
	assert.NoError(t, err)
	// a regular file where a directory is needed makes MkdirAll fail
	assert.NoError(t, os.WriteFile(filepath.Join(root, "blocked"), []byte("x"), 0o644))
	assert.Error(t, c.Put("blocked/1.seg", strings.NewReader("x")))
	assert.Equal(t, int64(0), c.Size())

	_, err = newFileCache(filepath.Join(root, "blocked", "sub"), 100)
	assert.Error(t, err)
}
