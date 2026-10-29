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
	"errors"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/core"
)

// fakeTopology is a hand-set cluster view.
type fakeTopology struct {
	mu        sync.Mutex
	local     string
	owners    map[string]string
	replicas  map[string][]string
	insync    map[string][]string
	insyncErr error
}

func newFakeTopology(local string) *fakeTopology {
	return &fakeTopology{
		local:    local,
		owners:   map[string]string{},
		replicas: map[string][]string{},
		insync:   map[string][]string{},
	}
}

func (f *fakeTopology) set(index, owner string, replicas ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners[index] = owner
	f.replicas[index] = replicas
}

func (f *fakeTopology) LocalID() string { return f.local }

func (f *fakeTopology) Owner(index string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	owner, ok := f.owners[index]
	return owner, ok
}

func (f *fakeTopology) IsOwner(index string) bool {
	owner, ok := f.Owner(index)
	return ok && owner == f.local
}

func (f *fakeTopology) IsReplica(index string) bool {
	return slices.Contains(f.Replicas(index), f.local)
}

func (f *fakeTopology) Replicas(index string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.replicas[index]...)
}

func (f *fakeTopology) Epoch(string) int64 { return 7 }

func (f *fakeTopology) UpdateInSync(_ context.Context, index string, nodes []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insyncErr != nil {
		return f.insyncErr
	}
	if nodes == nil {
		delete(f.insync, index)
		return nil
	}
	nodes = append([]string{}, nodes...)
	sort.Strings(nodes)
	f.insync[index] = nodes
	return nil
}

func (f *fakeTopology) InSync(context.Context) (map[string][]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string][]string, len(f.insync))
	for k, v := range f.insync {
		out[k] = append([]string{}, v...)
	}
	return out, nil
}

func (f *fakeTopology) inSyncOf(index string) ([]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	nodes, ok := f.insync[index]
	return nodes, ok
}

// loopTransport delivers batches to replica managers of the same process,
// or fails for nodes listed in down.
type loopTransport struct {
	mu        sync.Mutex
	nodes     map[string]*manager
	down      map[string]bool
	batches   []*Batch
	recovered []string
	snapshot  func(owner string, req *SnapshotRequest, dir string) (*SnapshotInfo, error)
}

func newLoopTransport() *loopTransport {
	return &loopTransport{nodes: map[string]*manager{}, down: map[string]bool{}}
}

func (l *loopTransport) Replicate(_ context.Context, node string, batch *Batch) (Ack, error) {
	l.mu.Lock()
	l.batches = append(l.batches, batch)
	target, down := l.nodes[node], l.down[node]
	l.mu.Unlock()
	if down || target == nil {
		return Ack{}, errors.New("connection refused")
	}
	return target.apply(batch)
}

func (l *loopTransport) Recover(_ context.Context, node, index string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recovered = append(l.recovered, node+"/"+index)
	return nil
}

func (l *loopTransport) Snapshot(_ context.Context, owner string, req *SnapshotRequest, dir string) (*SnapshotInfo, error) {
	if l.snapshot == nil {
		return nil, errors.New("no snapshot source")
	}
	return l.snapshot(owner, req, dir)
}

func (l *loopTransport) recoveries() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.recovered...)
}

// newManager builds a manager without the background loop or core hooks.
func newManager(t *testing.T, mode string, topo topology, transport Transport) *manager {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	m := &manager{
		mode:      mode,
		interval:  time.Hour,
		topo:      topo,
		transport: transport,
		primaries: map[string]*primary{},
		replicas:  map[string]*replica{},
		ctx:       ctx,
		cancel:    cancel,
	}
	t.Cleanup(func() {
		cancel()
		m.wg.Wait()
	})
	return m
}

// newDiskIndex creates and registers a disk index.
func newDiskIndex(t *testing.T, name string) *core.Index {
	t.Helper()
	index, err := core.NewIndex(name, core.StorageTypeDisk, 1)
	require.NoError(t, err)
	require.NoError(t, core.StoreIndex(index))
	core.ZED_INDEX_LIST.Add(index)
	t.Cleanup(func() { core.ZED_INDEX_LIST.Delete(name) })
	return index
}

func countDocs(t *testing.T, index *core.Index) uint64 {
	t.Helper()
	readers, err := index.GetReaders(0, 0)
	require.NoError(t, err)
	var total uint64
	for _, r := range readers {
		n, err := r.Count()
		require.NoError(t, err)
		total += n
		require.NoError(t, r.Close())
	}
	return total
}

func waitDocs(t *testing.T, index *core.Index, want uint64) {
	t.Helper()
	require.Eventually(t, func() bool { return countDocs(t, index) == want },
		30*time.Second, 50*time.Millisecond)
}

// recordWith routes core.WALHook to m for the test.
func recordWith(t *testing.T, m *manager) {
	t.Helper()
	core.WALHook = m.record
	t.Cleanup(func() { core.WALHook = nil })
}
