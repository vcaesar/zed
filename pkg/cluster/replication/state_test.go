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
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A restarted replica resumes from the persisted position instead of
// copying the index again; a promotion forgets the position.
func TestReplicaStateSurvivesRestart(t *testing.T) {
	name := "TestReplicaStateSurvivesRestart.index_1"
	index := newDiskIndex(t, name)
	require.NoError(t, removeState(name)) // left by an earlier run
	require.NoError(t, index.CreateDocument("1", map[string]interface{}{"name": "one"}, false))
	waitDocs(t, index, 1)

	topo := newFakeTopology("node-b")
	topo.set(name, "node-a", "node-b")
	m := newManager(t, ModeDocument, topo, newLoopTransport())
	r := m.replicaFor(name)
	assert.False(t, r.isReady(), "nothing persisted yet")

	r.mu.Lock()
	r.term, r.seq, r.filesSeq, r.ready = "t1", 5, 2, true
	r.mu.Unlock()
	m.replicaTick(name)
	assert.Equal(t, replicaState{Mode: ModeDocument, Term: "t1", Seq: 5, FilesSeq: 2}, r.saved)
	stat, err := os.Stat(statePath(name))
	require.NoError(t, err)
	m.replicaTick(name)
	again, err := os.Stat(statePath(name))
	require.NoError(t, err)
	assert.Equal(t, stat.ModTime(), again.ModTime(), "an unchanged position is not written again")

	// the restarted node
	restarted := newManager(t, ModeDocument, topo, newLoopTransport())
	got := restarted.replicaFor(name)
	assert.True(t, got.isReady())
	assert.Equal(t, "t1", got.term)
	assert.Equal(t, uint64(5), got.seq)
	assert.Equal(t, uint64(2), got.filesSeq)

	// a position recorded in another mode is not trusted
	segment := newManager(t, ModeSegment, topo, newLoopTransport())
	assert.False(t, segment.replicaFor(name).isReady())

	// the copy takes local writes once promoted
	topo.set(name, "node-b")
	_, err = restarted.prepare(context.Background(), name)
	require.NoError(t, err)
	_, err = os.Stat(statePath(name))
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.NoError(t, removeState(name), "removing twice is fine")
}

// A segment replica records the installed files only: kept operations are
// in memory and lost with the process.
func TestSegmentReplicaPersistsFiles(t *testing.T) {
	name := "TestSegmentReplicaPersistsFiles.index_1"
	newDiskIndex(t, name)
	require.NoError(t, os.RemoveAll(indexDir(name)))
	r := &replica{term: "t1", seq: 9, filesSeq: 4, ready: true}
	require.NoError(t, r.persist(name, ModeSegment))
	assert.Equal(t, replicaState{}, r.saved, "without local data there is nothing to record")

	require.NoError(t, os.MkdirAll(indexDir(name), 0o750))
	require.NoError(t, r.persist(name, ModeSegment))
	st, ok := loadState(name, ModeSegment)
	require.True(t, ok)
	assert.Equal(t, replicaState{Mode: ModeSegment, Term: "t1", Seq: 4, FilesSeq: 4}, st)

	assert.NoError(t, (&replica{}).persist(name, ModeSegment), "a copy not installed is not recorded")
	_, ok = loadState("TestSegmentReplicaPersistsFiles.missing", ModeSegment)
	assert.False(t, ok)

	require.NoError(t, os.WriteFile(statePath(name), []byte("{"), 0o640))
	_, ok = loadState(name, ModeSegment)
	assert.False(t, ok, "a broken record is ignored")
}

// Stop records the position of every replica, a graceful restart resumes
// exactly where it stopped.
func TestStopPersistsReplicas(t *testing.T) {
	name := "TestStopPersistsReplicas.index_1"
	newDiskIndex(t, name)
	require.NoError(t, os.MkdirAll(indexDir(name), 0o750))
	m := start(context.Background(), ModeDocument, time.Hour, newFakeTopology("node-b"), newLoopTransport())
	r := m.replicaFor(name)
	r.mu.Lock()
	r.term, r.seq, r.ready = "t1", 3, true
	r.mu.Unlock()
	Stop()
	st, ok := loadState(name, ModeDocument)
	require.True(t, ok)
	assert.Equal(t, uint64(3), st.Seq)
}
