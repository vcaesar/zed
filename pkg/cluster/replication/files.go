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

package replication

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/vcaesar/riot/index"

	"github.com/vcaesar/zedsearch/pkg/config"
)

// segmentExt names the immutable segment files a recovery may reuse.
const segmentExt = ".seg"

// reusableFile reports whether a recovery may reuse the file at path: a
// segment, or the HNSW graph riot persists for one. Both are matched by
// content, so a graph rebuilt since is copied again.
func reusableFile(path string) bool {
	ext := filepath.Ext(path)
	return ext == segmentExt || ext == index.ItemKindGraph
}

// FileSum identifies a segment file of a local copy by content.
type FileSum struct {
	Path string `json:"path"` // slash separated, relative to the index directory
	Size int64  `json:"size"`
	Sum  string `json:"sum"` // hex sha256
}

// sumCache remembers the sha256 of files by path, size and modification
// time: segments never change, so each is read once.
type sumCache struct {
	mu      sync.Mutex
	entries map[string]cachedSum
}

type cachedSum struct {
	size int64
	mod  time.Time
	sum  string
}

// sum returns the sha256 and size of the file at path, cached under key.
func (c *sumCache) sum(key, path string) (string, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	c.mu.Lock()
	e, ok := c.entries[key]
	c.mu.Unlock()
	if ok && e.size == info.Size() && e.mod.Equal(info.ModTime()) {
		return e.sum, e.size, nil
	}

	sum, err := fileSum(path)
	if err != nil {
		return "", 0, err
	}
	e = cachedSum{size: info.Size(), mod: info.ModTime(), sum: sum}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[string]cachedSum)
	}
	c.entries[key] = e
	c.mu.Unlock()
	return e.sum, e.size, nil
}

// fileSum returns the hex sha256 of the file at path.
func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	if err = errors.Join(err, f.Close()); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// prune forgets the files that are gone, merged away segments.
func (c *sumCache) prune() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if _, err := os.Stat(key); err != nil {
			delete(c.entries, key)
		}
	}
}

func indexDir(name string) string {
	return filepath.Join(config.Global.DataPath, name)
}

// stageSegments hard links the segment files of the local copy of an index
// into dir and returns their sums. The owner skips sending the files it
// has too, the recovery takes them from dir, where no merge removes them.
// A file that cannot be linked is left out and copied again.
func stageSegments(name, dir string, cache *sumCache) ([]FileSum, error) {
	root := indexDir(name)
	var files []FileSum
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil // no local copy yet, or a merge removed the file
			}
			return err
		}
		if entry.IsDir() || !reusableFile(path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		if os.Link(path, target) != nil {
			return nil
		}
		sum, size, err := cache.sum(path, target)
		if err != nil {
			return err
		}
		files = append(files, FileSum{Path: filepath.ToSlash(rel), Size: size, Sum: sum})
		return nil
	})
	cache.prune()
	return files, err
}

// reusable lists the files of the snapshot in dir the replica holds with
// the same content. Segments are immutable, so the live file of the owner
// under the same name is the one copied into dir and its sum is cached;
// one merged away since the copy is read from dir.
func reusable(name, dir string, held []FileSum, cache *sumCache) []string {
	root := indexDir(name)
	var reused []string
	for _, f := range held {
		rel := filepath.FromSlash(f.Path)
		if !filepath.IsLocal(rel) || !reusableFile(rel) {
			continue
		}
		if stat, err := os.Stat(filepath.Join(dir, rel)); err != nil || stat.Size() != f.Size {
			continue // not part of the snapshot
		}
		live := filepath.Join(root, rel)
		sum, _, err := cache.sum(live, live)
		if errors.Is(err, fs.ErrNotExist) {
			sum, err = fileSum(filepath.Join(dir, rel))
		}
		if err == nil && sum == f.Sum {
			reused = append(reused, f.Path)
		}
	}
	cache.prune()
	return reused
}
