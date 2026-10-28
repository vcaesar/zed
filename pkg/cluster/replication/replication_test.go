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
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/bluge/directory"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// twoNodes wires an owner manager (node-a) and a replica manager (node-b)
// over one disk index each; the replica's copy is a separate index of the
// same layout installed through a snapshot of the owner.
type twoNodes struct {
	transport *loopTransport
	ownerTopo *fakeTopology
	owner     *manager
	replica   *manager
	source    *core.Index
	name      string
}

func setupTwoNodes(t *testing.T, mode, name string) *twoNodes {
	t.Helper()
	env := &twoNodes{transport: newLoopTransport(), name: name}
	env.ownerTopo = newFakeTopology("node-a")
	env.ownerTopo.set(name, "node-a", "node-b")
	replicaTopo := newFakeTopology("node-b")
	replicaTopo.set(name, "node-a", "node-b")

	env.owner = newManager(t, mode, env.ownerTopo, env.transport)
	env.replica = newManager(t, mode, replicaTopo, env.transport)
	env.transport.nodes["node-b"] = env.replica
	env.source = newDiskIndex(t, name)
	recordWith(t, env.owner)
	return env
}

// bootstrapReplica marks the replica as following the owner's term from
// operation 0, the state a fresh replica reaches after its first recovery.
func (env *twoNodes) bootstrapReplica(t *testing.T) {
	t.Helper()
	p, err := env.owner.prepare(context.Background(), env.name)
	require.NoError(t, err)
	r := env.replica.replicaFor(env.name)
	r.term, r.ready = p.term, true
}

func TestModeFromConfig(t *testing.T) {
	previous := config.Global.Cluster.Replication
	t.Cleanup(func() { config.Global.Cluster.Replication = previous })
	for value, want := range map[string]string{"": ModeDocument, "document": ModeDocument, "segment": ModeSegment, "none": ModeNone} {
		config.Global.Cluster.Replication = value
		got, err := configuredMode()
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	config.Global.Cluster.Replication = "bogus"
	_, err := configuredMode()
	assert.Error(t, err)

	// clustering is off in the test binary: Start is a no-op
	require.NoError(t, Start(context.Background()))
	assert.False(t, Enabled())
	assert.Equal(t, ModeNone, Mode())
	assert.NoError(t, Commit(context.Background(), "x"))
	assert.NoError(t, CommitAll(context.Background()))
	assert.NoError(t, Prepare(context.Background(), "x"))
	assert.NoError(t, PrepareAll(context.Background()))
	assert.False(t, Ready("x"))
	_, err = Apply(&Batch{})
	assert.Error(t, err)
	assert.Error(t, RequestRecovery("x"))
}

func TestStartStopInstallsHooks(t *testing.T) {
	m := start(context.Background(), ModeDocument, time.Hour, newFakeTopology("node-a"), newLoopTransport())
	assert.True(t, Enabled())
	assert.Equal(t, ModeDocument, Mode())
	assert.NotNil(t, core.WALHook)
	assert.NotNil(t, core.StoreGuard)
	assert.NotNil(t, core.WriteGuard)
	assert.True(t, m.mayStore("anything"))
	Stop()
	assert.False(t, Enabled())
	assert.Nil(t, core.WALHook)
	assert.Nil(t, core.StoreGuard)
	assert.Nil(t, core.WriteGuard)
	Stop() // idempotent
}

// A local write to an index another node owns (a `_bulk` line served on
// the wrong node) is refused instead of diverging from the owner.
func TestMayWrite(t *testing.T) {
	topo := newFakeTopology("node-b")
	topo.set("remote", "node-a", "node-b")
	topo.set("local", "node-b", "node-a")
	m := newManager(t, ModeDocument, topo, newLoopTransport())
	assert.Error(t, m.mayWrite("remote"))
	assert.NoError(t, m.mayWrite("local"))
	assert.NoError(t, m.mayWrite("unassigned"))
}

// A replica drops its copy of an index whose metadata the owner deleted,
// and keeps the others.
func TestTickDropsDeletedIndex(t *testing.T) {
	kept := newDiskIndex(t, "TestTickDropsDeletedIndex.index_1")
	deleted, err := core.NewIndex("TestTickDropsDeletedIndex.index_2", core.StorageTypeDisk, 1)
	require.NoError(t, err)
	core.ZED_INDEX_LIST.Add(deleted) // never stored: the owner deleted it
	t.Cleanup(func() { core.ZED_INDEX_LIST.Delete(deleted.GetName()) })

	m := newManager(t, ModeDocument, newFakeTopology("node-b"), newLoopTransport())
	m.replicaFor(deleted.GetName())
	m.tick(context.Background())

	_, ok := core.GetIndex(deleted.GetName())
	assert.False(t, ok)
	assert.NotContains(t, m.replicas, deleted.GetName())
	_, ok = core.GetIndex(kept.GetName())
	assert.True(t, ok)
}

// Document replication: a write is acknowledged once the in-sync replica
// indexed it, and the replica ends up with the same documents.
func TestDocumentReplication(t *testing.T) {
	env := setupTwoNodes(t, ModeDocument, "TestDocumentReplication.index_1")
	ctx := context.Background()
	// both nodes share one process and one index: the replica applies the
	// shipped entries to the owner's index, idempotently, bypassing the hook
	env.bootstrapReplica(t)
	_, ok := env.ownerTopo.inSyncOf(env.name)
	require.True(t, ok, "promotion creates the in-sync entry")

	source := env.source
	require.NoError(t, source.CreateDocument("1", map[string]interface{}{"name": "one"}, false))
	require.NoError(t, source.CreateDocument("2", map[string]interface{}{"name": "two"}, false))

	p := env.owner.primaries[env.name]
	require.NoError(t, env.owner.commit(ctx, p, false))
	nodes, _ := env.ownerTopo.inSyncOf(env.name)
	assert.Equal(t, []string{"node-b"}, nodes, "a caught up replica joins the in-sync set")
	assert.Equal(t, uint64(2), env.replica.replicaFor(env.name).seq)

	// every entry is idempotent, the index holds each document once
	waitDocs(t, source, 2)

	// a commit without new operations does not call the replica
	calls := len(env.transport.batches)
	require.NoError(t, env.owner.commit(ctx, p, false))
	assert.Len(t, env.transport.batches, calls)
}

// OpenSearch's failShardIfNeeded: a replica that cannot take a write is
// removed from the in-sync set before the write is acknowledged, and the
// write fails when that removal cannot be recorded.
func TestFailedReplicaIsMarkedStale(t *testing.T) {
	env := setupTwoNodes(t, ModeDocument, "TestFailedReplicaIsMarkedStale.index_1")
	ctx := context.Background()
	env.bootstrapReplica(t)
	p := env.owner.primaries[env.name]
	require.NoError(t, env.ownerTopo.UpdateInSync(ctx, env.name, []string{"node-b"}))
	p.insync = []string{"node-b"}

	env.transport.down["node-b"] = true
	env.owner.record(env.name, "0", []byte(`{"@_id":"1","@_action":"insert","@_shard":-1}`))

	env.ownerTopo.insyncErr = errors.New("etcd down")
	assert.Error(t, env.owner.commit(ctx, p, false), "a stale replica that cannot be recorded fails the write")

	env.ownerTopo.insyncErr = nil
	require.NoError(t, env.owner.commit(ctx, p, false))
	nodes, ok := env.ownerTopo.inSyncOf(env.name)
	assert.True(t, ok)
	assert.Empty(t, nodes)
}

// A replica that missed operations is caught up from the retained log; one
// on another term is asked to recover from files.
func TestCatchUpAndRecovery(t *testing.T) {
	env := setupTwoNodes(t, ModeSegment, "TestCatchUpAndRecovery.index_1")
	ctx := context.Background()
	env.bootstrapReplica(t)
	p := env.owner.primaries[env.name]

	for range 3 {
		env.owner.record(env.name, "0", []byte(`{"@_id":"1","@_action":"update","@_shard":-2}`))
	}
	// the owner wrongly believes the replica already has operation 2
	p.acked["node-b"] = 2
	require.NoError(t, env.owner.commit(ctx, p, false))
	r := env.replica.replicaFor(env.name)
	assert.Equal(t, uint64(3), r.seq)
	assert.Len(t, r.pending, 3, "segment replicas keep operations without indexing them")
	assert.Empty(t, env.transport.recoveries())

	// another history: recover
	r.mu.Lock()
	r.term = "other"
	r.mu.Unlock()
	env.owner.record(env.name, "0", []byte(`{"@_id":"1","@_action":"update","@_shard":-2}`))
	require.NoError(t, env.owner.commit(ctx, p, false))
	assert.Equal(t, []string{"node-b/" + env.name}, env.transport.recoveries())
}

func TestApplyRejections(t *testing.T) {
	topo := newFakeTopology("node-b")
	topo.set("TestApplyRejections.index_1", "node-a", "node-b")
	m := newManager(t, ModeDocument, topo, newLoopTransport())

	ack, err := m.apply(&Batch{Index: "TestApplyRejections.index_1", Term: "t1", From: 1})
	assert.ErrorIs(t, err, ErrOutOfSync, "not installed yet")
	assert.Zero(t, ack.Seq)

	r := m.replicaFor("TestApplyRejections.index_1")
	r.term, r.ready, r.seq = "t1", true, 4
	_, err = m.apply(&Batch{Index: "TestApplyRejections.index_1", Term: "t1", From: 9})
	assert.ErrorIs(t, err, ErrOutOfSync, "gap")
	ack, err = m.apply(&Batch{Index: "TestApplyRejections.index_1", Term: "t1", From: 3, Ops: []Op{{Seq: 3}, {Seq: 4}}})
	require.NoError(t, err, "duplicates are skipped")
	assert.Equal(t, uint64(4), ack.Seq)
	_, err = m.apply(&Batch{Index: "TestApplyRejections.index_1", Term: "t1", From: 5, Ops: []Op{{Seq: 5, Shard: "0", Entry: json.RawMessage(`{}`)}}})
	assert.Error(t, err, "unknown index")

	topo.set("TestApplyRejections.index_1", "node-b")
	_, err = m.apply(&Batch{Index: "TestApplyRejections.index_1"})
	assert.Error(t, err, "an owner never applies batches")
}

func TestPrimaryTrimAndOpsFrom(t *testing.T) {
	p := &primary{acked: map[string]uint64{}}
	for i := uint64(1); i <= 5; i++ {
		p.seq = i
		p.ops = append(p.ops, Op{Seq: i})
	}
	ops, ok := p.opsFrom(2, 4)
	require.True(t, ok)
	assert.Equal(t, []Op{{Seq: 2}, {Seq: 3}, {Seq: 4}}, ops)
	_, ok = p.opsFrom(6, 5)
	assert.True(t, ok, "an empty range is always available")

	p.acked["n1"] = 3
	p.trim([]string{"n1"})
	_, ok = p.opsFrom(3, 5)
	assert.False(t, ok)
	ops, ok = p.opsFrom(4, 5)
	require.True(t, ok)
	assert.Len(t, ops, 2)
	assert.True(t, p.retained(4, 5))

	p.insync = []string{"n1"}
	assert.True(t, p.dirty())
	p.acked["n1"] = 5
	assert.False(t, p.dirty())
}

// Segment replication end to end on one process: the owner's files are
// installed on a replica copy, kept operations are replayed on promotion.
func TestSegmentCheckpointAndPromotion(t *testing.T) {
	name := "TestSegmentCheckpointAndPromotion.index_1"
	env := setupTwoNodes(t, ModeSegment, name)
	ctx := context.Background()
	require.NoError(t, env.source.CreateDocument("1", map[string]interface{}{"name": "one"}, false))
	waitDocs(t, env.source, 1)

	// the owner serves the snapshot through the real tar format
	activeMu.Lock()
	active = env.owner
	activeMu.Unlock()
	t.Cleanup(func() {
		activeMu.Lock()
		active = nil
		activeMu.Unlock()
	})
	env.transport.snapshot = func(_, index string, have Checkpoint, dir string) (*SnapshotInfo, error) {
		var buf bytes.Buffer
		if err := ServeSnapshot(ctx, index, have, func() io.Writer { return &buf }); err != nil {
			return nil, err
		}
		return ReadSnapshot(&buf, dir)
	}

	// the replica (same process) installs the copy over the index
	r := env.replica.replicaFor(name)
	have := Checkpoint{}
	require.NoError(t, env.replica.recover(name, r, have))
	assert.True(t, r.isReady())
	installed, ok := core.GetIndex(name)
	require.True(t, ok)
	waitDocs(t, installed, 1)

	// nothing new: the owner answers not modified
	term, seq := env.owner.primaries[name].checkpoint()
	assert.ErrorIs(t, env.replica.recover(name, r, Checkpoint{Term: term, Seq: seq}), ErrNotModified)

	// kept operations are indexed when the replica becomes owner
	r.pending = []Op{{Seq: seq + 1, Shard: firstShard(t, installed), Entry: json.RawMessage(`{"@_id":"2","@_action":"insert","@_shard":-1,"name":"two"}`)}}
	env.replica.topo.(*fakeTopology).set(name, "node-b", "node-a")
	core.WALHook = nil
	_, err := env.replica.prepare(ctx, name)
	require.NoError(t, err)
	waitDocs(t, installed, 2)
	assert.Nil(t, env.replica.replicas[name], "the replica state is gone after promotion")
}

func firstShard(t *testing.T, index *core.Index) string {
	t.Helper()
	data, err := index.MarshalJSON()
	require.NoError(t, err)
	var stored struct {
		Shards map[string]any `json:"shards"`
	}
	require.NoError(t, json.Unmarshal(data, &stored))
	for id := range stored.Shards {
		return id
	}
	t.Fatal("index has no shard")
	return ""
}

func TestSnapshotTarRoundTrip(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "a", "000000"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a", "000000", "1.seg"), []byte("segment"), 0o640))

	var buf bytes.Buffer
	info := &SnapshotInfo{Checkpoint: Checkpoint{Term: "t", Seq: 3}, Index: json.RawMessage(`{"name":"x"}`)}
	require.NoError(t, WriteSnapshot(&buf, info, src))

	dst := t.TempDir()
	got, err := ReadSnapshot(&buf, dst)
	require.NoError(t, err)
	assert.Equal(t, info.Checkpoint, got.Checkpoint)
	data, err := os.ReadFile(filepath.Join(dst, "a", "000000", "1.seg"))
	require.NoError(t, err)
	assert.Equal(t, "segment", string(data))

	_, err = ReadSnapshot(bytes.NewReader(nil), dst)
	assert.Error(t, err, "an archive without metadata is rejected")
	assert.True(t, CheckSecret("s", "s"))
	assert.False(t, CheckSecret("", ""))
	assert.False(t, CheckSecret("x", "s"))
}

// Remote-backed storage: commits upload the operations to the bucket, a
// new owner replays them, a flush trims what the segments hold.
func TestRemoteTranslog(t *testing.T) {
	store := directory.NewMemoryObjectStore()
	restore, err := directory.SetTestObjectStore(core.StorageTypeS3, store)
	require.NoError(t, err)
	t.Cleanup(restore)
	oldBucket := config.Global.S3.Bucket
	config.Global.S3.Bucket = "zed-test"
	t.Cleanup(func() { config.Global.S3.Bucket = oldBucket })

	name := "TestRemoteTranslog.index_1"
	index, err := core.NewIndex(name, core.StorageTypeS3, 1)
	require.NoError(t, err)
	require.NoError(t, core.StoreIndex(index))
	core.ZED_INDEX_LIST.Add(index)
	t.Cleanup(func() { core.ZED_INDEX_LIST.Delete(name) })

	topo := newFakeTopology("node-a")
	topo.set(name, "node-a", "node-b")
	transport := newLoopTransport()
	m := newManager(t, ModeDocument, topo, transport)
	recordWith(t, m)
	ctx := context.Background()

	require.NoError(t, index.CreateDocument("1", map[string]interface{}{"name": "one"}, false))
	p, err := m.prepare(ctx, name)
	require.NoError(t, err)
	assert.True(t, p.remote)
	require.NoError(t, m.commit(ctx, p, false))
	assert.Equal(t, 1, store.Len(translogPrefix(name)))
	assert.Empty(t, transport.batches, "remote indexes ship nothing to replicas")
	_, replicated := topo.inSyncOf(name)
	assert.False(t, replicated, "the bucket, not a replica, holds remote writes")

	// another node takes over and replays the translog
	core.WALHook = nil
	other := newManager(t, ModeDocument, newFakeTopology("node-b"), transport)
	other.topo.(*fakeTopology).set(name, "node-b")
	takeover, err := other.prepare(ctx, name)
	require.NoError(t, err)
	waitDocs(t, index, 1)

	require.NoError(t, other.flushRemote(ctx, takeover))
	assert.Zero(t, store.Len(translogPrefix(name)), "older terms are trimmed once flushed")

	term, last, ok := parseTranslogKey(translogPrefix(name), translogKey(p, 42))
	require.True(t, ok)
	assert.Equal(t, p.term, term)
	assert.Equal(t, uint64(42), last)
	_, _, ok = parseTranslogKey("x/", "x/bad")
	assert.False(t, ok)
}

func TestCleanupDropsDeletedIndexes(t *testing.T) {
	topo := newFakeTopology("node-a")
	topo.set("TestCleanupDropsDeletedIndexes.gone", "node-a")
	require.NoError(t, topo.UpdateInSync(context.Background(), "TestCleanupDropsDeletedIndexes.gone", []string{}))
	m := newManager(t, ModeDocument, topo, newLoopTransport())
	m.replicas["TestCleanupDropsDeletedIndexes.other"] = new(replica)

	m.cleanup(context.Background())
	_, ok := topo.inSyncOf("TestCleanupDropsDeletedIndexes.gone")
	assert.False(t, ok)
	assert.Empty(t, m.replicas)
}
