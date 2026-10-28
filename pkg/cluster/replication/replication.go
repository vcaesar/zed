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

// Package replication keeps the data of every index on more than one node,
// following the three OpenSearch replication paths:
//
//   - document: the owner forwards every WAL entry to its replicas and
//     acknowledges a write once every in-sync replica appended it; replicas
//     index the entries themselves (OpenSearch document replication)
//   - segment: entries are forwarded the same way for durability but the
//     replicas only keep them; they copy the owner's segments on every
//     checkpoint and replay the kept entries when promoted (segment
//     replication)
//   - remote: indexes on object storage upload their entries to the bucket
//     before a write is acknowledged; segments already live there, so a new
//     owner replays the remote translog (remote-backed storage)
//
// document and segment apply to disk indexes and are chosen with
// `zed_cluster_replication`; remote is used for every writable index on
// object storage.
package replication

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/cluster"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/metadata"
)

// Replication modes of disk indexes.
const (
	ModeDocument = "document"
	ModeSegment  = "segment"
	ModeNone     = "none"
)

const (
	// retainOps is how many committed operations the owner keeps to catch
	// a lagging replica up without copying files.
	retainOps = 100000
	// callTimeout bounds one node to node replication call.
	callTimeout = 30 * time.Second
	// snapshotTimeout bounds a full file copy.
	snapshotTimeout = 30 * time.Minute
)

// ErrOutOfSync is returned by a replica that cannot apply a batch: other
// term, missing operations, or a recovery in progress.
var ErrOutOfSync = errors.New("replication: replica out of sync")

// topology is the slice of the cluster the replication layer reads.
type topology interface {
	LocalID() string
	Owner(index string) (string, bool)
	IsOwner(index string) bool
	IsReplica(index string) bool
	Replicas(index string) []string
	Epoch(index string) int64
	UpdateInSync(ctx context.Context, index string, nodes []string) error
	InSync(ctx context.Context) (map[string][]string, error)
}

// clusterTopology answers from the running cluster.
type clusterTopology struct{}

func (clusterTopology) LocalID() string {
	if node := cluster.Local(); node != nil {
		return node.ID
	}
	return ""
}

func (clusterTopology) Owner(index string) (string, bool) {
	owner, _, ok := cluster.Owner(index)
	return owner, ok
}

func (t clusterTopology) IsOwner(index string) bool {
	owner, ok := t.Owner(index)
	return ok && owner == t.LocalID() && cluster.IsLocalOwner(index)
}

func (clusterTopology) IsReplica(index string) bool { return cluster.IsLocalReplica(index) }

func (clusterTopology) Replicas(index string) []string { return cluster.Replicas(index) }

func (clusterTopology) Epoch(index string) int64 {
	_, epoch, _ := cluster.Owner(index)
	return epoch
}

func (clusterTopology) UpdateInSync(ctx context.Context, index string, nodes []string) error {
	return cluster.UpdateInSync(ctx, index, nodes)
}

func (clusterTopology) InSync(ctx context.Context) (map[string][]string, error) {
	return cluster.InSync(ctx)
}

// manager is the replication state of this node.
type manager struct {
	mode      string
	interval  time.Duration
	topo      topology
	transport Transport

	mu        sync.Mutex
	primaries map[string]*primary
	replicas  map[string]*replica

	ctx    context.Context // cancelled by Stop
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

var (
	activeMu sync.RWMutex
	active   *manager
)

func get() *manager {
	activeMu.RLock()
	defer activeMu.RUnlock()
	return active
}

// Enabled reports whether replication runs on this node.
func Enabled() bool {
	return get() != nil
}

// Mode returns the replication mode of disk indexes, ModeNone when off.
func Mode() string {
	if m := get(); m != nil {
		return m.mode
	}
	return ModeNone
}

func configuredMode() (string, error) {
	switch mode := config.Global.Cluster.Replication; mode {
	case "", ModeDocument:
		return ModeDocument, nil
	case ModeSegment, ModeNone:
		return mode, nil
	default:
		return "", fmt.Errorf("replication: zed_cluster_replication [%s] is invalid, just accept [document, segment, none]", mode)
	}
}

// Start turns replication on when the cluster is enabled and the mode is
// not none. It must run after cluster.Start.
func Start(ctx context.Context) error {
	if !cluster.Enabled() {
		return nil
	}
	mode, err := configuredMode()
	if err != nil {
		return err
	}
	if mode == ModeNone {
		return nil
	}
	if config.Global.Cluster.Secret == "" {
		return errors.New("replication: zed_cluster_secret is required, set it on every node or set zed_cluster_replication = \"none\"")
	}
	interval := config.Global.Cluster.ReplicationInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	start(ctx, mode, interval, clusterTopology{}, NewHTTPTransport(config.Global.Cluster.Secret))
	return nil
}

// start installs the core hooks and the background loop.
func start(ctx context.Context, mode string, interval time.Duration, topo topology, transport Transport) *manager {
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m := &manager{
		mode:      mode,
		interval:  interval,
		topo:      topo,
		transport: transport,
		primaries: make(map[string]*primary),
		replicas:  make(map[string]*replica),
		ctx:       runCtx,
		cancel:    cancel,
	}
	activeMu.Lock()
	active = m
	activeMu.Unlock()
	core.WALHook = m.record
	core.StoreGuard = m.mayStore
	core.WriteGuard = m.mayWrite

	m.wg.Add(1)
	go func() { defer m.wg.Done(); m.run(runCtx) }()
	log.Info().Str("mode", mode).Dur("interval", interval).Msg("replication: started")
	return m
}

// Stop removes the hooks and stops the background loop.
func Stop() {
	activeMu.Lock()
	m := active
	active = nil
	activeMu.Unlock()
	if m == nil {
		return
	}
	core.WALHook = nil
	core.StoreGuard = nil
	core.WriteGuard = nil
	m.cancel()
	m.wg.Wait()
}

// mayStore keeps a replica from writing the owner's index metadata.
func (m *manager) mayStore(index string) bool {
	m.mu.Lock()
	_, isReplica := m.replicas[index]
	m.mu.Unlock()
	return !isReplica || m.topo.IsOwner(index)
}

// mayWrite rejects a local write to an index another node owns, such as a
// `_bulk` line served here: it would never reach the replicas and the next
// recovery of this copy would erase it.
func (m *manager) mayWrite(index string) error {
	owner, ok := m.topo.Owner(index)
	if !ok || owner == m.topo.LocalID() {
		return nil
	}
	return fmt.Errorf("replication: index [%s] is owned by node %s, write to it there", index, owner)
}

// run is the background loop: promotions, probes of lagging replicas,
// segment checkpoints, remote translog flushes and cleanup.
func (m *manager) run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

func (m *manager) tick(ctx context.Context) {
	stored, err := storedIndexes()
	if err != nil {
		log.Warn().Err(err).Msg("replication: list index metadata")
	}
	for _, index := range core.ZED_INDEX_LIST.List() {
		name := index.GetName()
		switch {
		case stored != nil && !stored[name] && !m.topo.IsOwner(name):
			m.dropDeleted(name)
		case m.topo.IsOwner(name):
			m.ownerTick(ctx, index)
		case m.topo.IsReplica(name) && index.IsDisk():
			m.replicaTick(name)
		}
	}
	m.cleanup(ctx)
}

// storedIndexes returns the names in the shared index metadata.
func storedIndexes() (map[string]bool, error) {
	indexes, err := metadata.Index.List(0, 0)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(indexes))
	for _, index := range indexes {
		names[index.Name] = true
	}
	return names, nil
}

// dropDeleted removes the local copy of an index its owner deleted, so it
// is neither served from here nor resurrected when this node is promoted.
func (m *manager) dropDeleted(name string) {
	m.mu.Lock()
	r := m.replicas[name]
	m.mu.Unlock()
	if r != nil && r.isRecovering() {
		return
	}
	if err := core.DropLocalIndex(name); err != nil {
		log.Warn().Err(err).Str("index", name).Msg("replication: drop deleted index")
		return
	}
	m.mu.Lock()
	delete(m.replicas, name)
	m.mu.Unlock()
	log.Info().Str("index", name).Msg("replication: dropped the copy of a deleted index")
}

func (m *manager) ownerTick(ctx context.Context, index *core.Index) {
	name := index.GetName()
	if index.IsCold() {
		return
	}
	p, err := m.prepare(ctx, name)
	if err != nil {
		log.Error().Err(err).Str("index", name).Msg("replication: promote")
		return
	}
	if p.remote {
		if err := m.flushRemote(ctx, p); err != nil {
			log.Error().Err(err).Str("index", name).Msg("replication: remote flush")
		}
		return
	}
	if err := m.commit(ctx, p, true); err != nil && ctx.Err() == nil {
		log.Warn().Err(err).Str("index", name).Msg("replication: probe replicas")
	}
}

func (m *manager) replicaTick(name string) {
	r := m.replicaFor(name)
	if m.mode == ModeSegment || !r.isReady() {
		m.startRecovery(name)
	}
}

// cleanup drops the state of indexes this node no longer owns or
// replicates, and the in-sync entries of deleted indexes it owns.
func (m *manager) cleanup(ctx context.Context) {
	m.mu.Lock()
	for name := range m.primaries {
		if _, exists := core.GetIndex(name); !exists || !m.topo.IsOwner(name) {
			delete(m.primaries, name)
		}
	}
	for name, r := range m.replicas {
		if !r.isRecovering() && !m.topo.IsReplica(name) && !m.topo.IsOwner(name) {
			delete(m.replicas, name)
		}
	}
	m.mu.Unlock()

	insync, err := m.topo.InSync(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("replication: read in-sync set")
		return
	}
	for name := range insync {
		if _, exists := core.GetIndex(name); exists || !m.topo.IsOwner(name) {
			continue
		}
		if err := m.topo.UpdateInSync(ctx, name, nil); err != nil {
			log.Warn().Err(err).Str("index", name).Msg("replication: drop in-sync entry")
		}
	}
}

// Prepare must run before a write to index is admitted on its owner: a
// replica that just became owner first replays the operations it kept
// (segment) or the remote translog (remote).
func Prepare(ctx context.Context, index string) error {
	m := get()
	if m == nil || !m.writable(index) {
		return nil
	}
	_, err := m.prepare(ctx, index)
	return err
}

// PrepareAll prepares every writable index owned here, for writes that
// name their indexes in the body (`_bulk`): a promoted node must replay
// what it kept before such a write lands.
func PrepareAll(ctx context.Context) error {
	m := get()
	if m == nil {
		return nil
	}
	var errs []error
	for _, index := range core.ZED_INDEX_LIST.List() {
		if name := index.GetName(); m.writable(name) {
			_, err := m.prepare(ctx, name)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// writable reports whether index is an existing writable index owned here.
// A missing one is about to be created by the request, nothing to replay.
func (m *manager) writable(name string) bool {
	if !m.topo.IsOwner(name) {
		return false
	}
	index, err := core.LoadIndex(name)
	return err == nil && !index.IsCold()
}

// Commit returns once every operation recorded for index is on all in-sync
// replicas (document, segment) or in the bucket (remote).
func Commit(ctx context.Context, index string) error {
	m := get()
	if m == nil {
		return nil
	}
	if !m.writable(index) {
		return nil // nothing was recorded here
	}
	p, err := m.prepare(ctx, index)
	if err != nil {
		return err
	}
	return m.commit(ctx, p, false)
}

// CommitAll commits every index with recorded operations, for writes that
// name their indexes in the body (`_bulk`).
func CommitAll(ctx context.Context) error {
	m := get()
	if m == nil {
		return nil
	}
	m.mu.Lock()
	pending := make([]string, 0, len(m.primaries))
	for name, p := range m.primaries {
		if p.dirty() {
			pending = append(pending, name)
		}
	}
	m.mu.Unlock()
	var errs []error
	for _, name := range pending {
		errs = append(errs, Commit(ctx, name))
	}
	return errors.Join(errs...)
}

// Ready reports whether this node holds a usable copy of index as a
// replica, so it may serve reads of it.
func Ready(index string) bool {
	m := get()
	if m == nil {
		return false
	}
	m.mu.Lock()
	r := m.replicas[index]
	m.mu.Unlock()
	return r != nil && r.isReady()
}
