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
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

func TestSumCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.seg")
	require.NoError(t, os.WriteFile(path, []byte("segment"), 0o640))

	var c sumCache
	sum, size, err := c.sum(path, path)
	require.NoError(t, err)
	assert.Equal(t, int64(7), size)
	assert.Len(t, sum, 64)
	again, _, err := c.sum(path, path)
	require.NoError(t, err)
	assert.Equal(t, sum, again)

	_, _, err = c.sum(path, filepath.Join(dir, "missing.seg"))
	assert.Error(t, err)

	require.NoError(t, os.Remove(path))
	c.prune()
	assert.Empty(t, c.entries, "gone files are forgotten")
}

// The owner leaves out of the snapshot the segments the replica holds with
// the same content, and only those.
func TestReusable(t *testing.T) {
	name := "TestReusable.index_1"
	live := indexDir(name)
	require.NoError(t, os.MkdirAll(filepath.Join(live, "s", "000000"), 0o750))
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(live)) })
	snapshot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(snapshot, "s", "000000"), 0o750))
	for file, data := range map[string]string{"1.seg": "same", "2.seg": "changed", "3.snp": "meta", "10001.hnsw": "graph"} {
		require.NoError(t, os.WriteFile(filepath.Join(live, "s", "000000", file), []byte(data), 0o640))
		require.NoError(t, os.WriteFile(filepath.Join(snapshot, "s", "000000", file), []byte(data), 0o640))
	}

	var held []FileSum
	var replica sumCache
	for _, file := range []string{"1.seg", "2.seg", "3.snp", "10001.hnsw"} {
		path := filepath.Join(live, "s", "000000", file)
		sum, size, err := replica.sum(path, path)
		require.NoError(t, err)
		held = append(held, FileSum{Path: "s/000000/" + file, Size: size, Sum: sum})
	}
	held[1].Sum = "other content"
	held = append(held,
		FileSum{Path: "../escape.seg", Size: 4, Sum: held[0].Sum},
		FileSum{Path: "s/000000/9.seg", Size: 4, Sum: held[0].Sum})

	var owner sumCache
	assert.Equal(t, []string{"s/000000/1.seg", "s/000000/10001.hnsw"}, reusable(name, snapshot, held, &owner))

	// merged away on the owner after the copy: the copy is compared
	require.NoError(t, os.Remove(filepath.Join(live, "s", "000000", "1.seg")))
	assert.Equal(t, []string{"s/000000/1.seg", "s/000000/10001.hnsw"}, reusable(name, snapshot, held, &owner))
}

// stageSegments links the local segments, so a merge cannot remove them
// before the recovery installs them.
func TestStageSegments(t *testing.T) {
	name := "TestStageSegments.index_1"
	live := indexDir(name)
	require.NoError(t, os.MkdirAll(filepath.Join(live, "s", "000000"), 0o750))
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(live)) })
	require.NoError(t, os.WriteFile(filepath.Join(live, "s", "000000", "1.seg"), []byte("segment"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(live, "s", "000000", "1.snp"), []byte("meta"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(live, "s", "000000", "10001.hnsw"), []byte("graph"), 0o640))

	var c sumCache
	// under the data path like a recovery: links do not cross drives
	staged := filepath.Join(indexDir("_replication_test"), name)
	require.NoError(t, os.RemoveAll(staged))
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(staged)) })
	files, err := stageSegments(name, staged, &c)
	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, "s/000000/1.seg", files[0].Path)
	assert.Equal(t, int64(7), files[0].Size)
	assert.Equal(t, "s/000000/10001.hnsw", files[1].Path, "graphs are offered too")

	require.NoError(t, os.Remove(filepath.Join(live, "s", "000000", "1.seg")))
	data, err := os.ReadFile(filepath.Join(staged, "s", "000000", "1.seg"))
	require.NoError(t, err)
	assert.Equal(t, "segment", string(data))

	files, err = stageSegments("TestStageSegments.missing", t.TempDir(), &c)
	require.NoError(t, err)
	assert.Empty(t, files, "no local copy, nothing to offer")
}

func TestWriteSnapshotSkipsReused(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "a", "000000"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a", "000000", "1.seg"), []byte("old"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a", "000000", "2.seg"), []byte("new"), 0o640))

	var buf bytes.Buffer
	info := &SnapshotInfo{Index: json.RawMessage(`{}`), Reused: []string{"a/000000/1.seg"}}
	require.NoError(t, WriteSnapshot(&buf, info, src))
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	var names []string
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		names = append(names, header.Name)
	}
	assert.Equal(t, []string{snapshotMetaName, "a/000000/2.seg"}, names)

	got, err := ReadSnapshot(&buf, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, info.Reused, got.Reused)
}

// A second recovery of a segment replica takes the segments it already has
// from its copy instead of receiving them again. No write happens between
// the two, so no background merge can replace the segment meanwhile.
func TestIncrementalRecovery(t *testing.T) {
	name := "TestIncrementalRecovery.index_1"
	env := setupTwoNodes(t, ModeSegment, name)
	ctx := context.Background()
	require.NoError(t, env.source.CreateDocument("1", map[string]interface{}{"name": "one"}, false))
	waitDocs(t, env.source, 1)

	activeMu.Lock()
	active = env.owner
	activeMu.Unlock()
	t.Cleanup(func() {
		activeMu.Lock()
		active = nil
		activeMu.Unlock()
	})
	var infos []*SnapshotInfo
	var held [][]FileSum
	var sent [][]string
	env.transport.snapshot = func(_ string, req *SnapshotRequest, dir string) (*SnapshotInfo, error) {
		if len(infos) == 0 {
			// both nodes share the data path here: the first copy acts as
			// a fresh replica, which holds none of the owner's segments
			req.Files = nil
		}
		var buf bytes.Buffer
		if err := ServeSnapshot(ctx, req, func() io.Writer { return &buf }); err != nil {
			return nil, err
		}
		names, err := tarNames(buf.Bytes())
		if err != nil {
			return nil, err
		}
		info, err := ReadSnapshot(&buf, dir)
		if err == nil {
			infos, held, sent = append(infos, info), append(held, req.Files), append(sent, names)
		}
		return info, err
	}

	r := env.replica.replicaFor(name)
	require.NoError(t, env.replica.recover(name, r, Checkpoint{}))
	installed, ok := core.GetIndex(name)
	require.True(t, ok)
	waitDocs(t, installed, 1)
	assert.Empty(t, infos[0].Reused, "a fresh replica reuses nothing")
	assert.Contains(t, segmentNames(sent[0]), true, "the first copy carries the segments")

	// an empty checkpoint forces a new copy although nothing changed
	require.NoError(t, env.replica.recover(name, r, Checkpoint{}))
	installed, ok = core.GetIndex(name)
	require.True(t, ok)
	waitDocs(t, installed, 1)

	require.Len(t, infos, 2)
	require.NotEmpty(t, held[1], "the replica offers the segments it holds")
	offered := make([]string, 0, len(held[1]))
	for _, f := range held[1] {
		offered = append(offered, f.Path)
	}
	assert.ElementsMatch(t, offered, infos[1].Reused, "every held segment is reused")
	assert.NotContains(t, segmentNames(sent[1]), true, "no segment is sent again")
	st, ok := loadState(name, ModeSegment)
	require.True(t, ok, "the installed copy is recorded")
	assert.Equal(t, infos[1].Seq, st.FilesSeq)
}

func tarNames(data []byte) ([]string, error) {
	tr := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names, nil
		}
		if err != nil {
			return nil, err
		}
		names = append(names, header.Name)
	}
}

// segmentNames maps each archive entry to whether it is a segment.
func segmentNames(names []string) []bool {
	segments := make([]bool, len(names))
	for i, name := range names {
		segments[i] = filepath.Ext(name) == segmentExt
	}
	return segments
}
