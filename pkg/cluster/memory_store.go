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

package cluster

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vcaesar/zedsearch/pkg/errors"
)

// MemoryStore is an in-process Store used by tests. Lease expiry is driven
// by a pluggable clock so TTL behaviour can be exercised without sleeping.
type MemoryStore struct {
	mu        sync.Mutex
	rev       int64
	kvs       map[string]KV
	keyLease  map[string]LeaseID
	leases    map[LeaseID]*memLease
	nextLease LeaseID
	watchers  []*memWatcher
	now       func() time.Time
	closed    bool
}

type memLease struct {
	ttl      time.Duration
	deadline time.Time
}

type memWatcher struct {
	prefix string
	ch     chan Event
	done   chan struct{}
}

// NewMemoryStore returns an empty store using the wall clock.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		kvs:      make(map[string]KV),
		keyLease: make(map[string]LeaseID),
		leases:   make(map[LeaseID]*memLease),
		now:      time.Now,
	}
}

// SetClock replaces the clock used for lease expiry, for deterministic tests.
func (m *MemoryStore) SetClock(now func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = now
}

func (m *MemoryStore) Grant(_ context.Context, ttl time.Duration) (LeaseID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return NoLease, ErrStoreClosed
	}
	m.expire()
	m.nextLease++
	m.leases[m.nextLease] = &memLease{ttl: ttl, deadline: m.now().Add(ttl)}
	return m.nextLease, nil
}

func (m *MemoryStore) KeepAlive(_ context.Context, id LeaseID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire()
	lease, ok := m.leases[id]
	if !ok {
		return errors.ErrKeyNotFound
	}
	lease.deadline = m.now().Add(lease.ttl)
	return nil
}

func (m *MemoryStore) Revoke(_ context.Context, id LeaseID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire()
	if _, ok := m.leases[id]; !ok {
		return nil
	}
	delete(m.leases, id)
	m.dropLeaseKeys(id)
	return nil
}

func (m *MemoryStore) Put(_ context.Context, key string, value []byte, lease LeaseID) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire()
	return m.put(key, value, lease), nil
}

func (m *MemoryStore) Get(_ context.Context, key string) (KV, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire()
	kv, ok := m.kvs[key]
	if !ok {
		return KV{}, errors.ErrKeyNotFound
	}
	return copyKV(kv), nil
}

func (m *MemoryStore) List(_ context.Context, prefix string) ([]KV, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire()
	out := make([]KV, 0, len(m.kvs))
	for key, kv := range m.kvs {
		if strings.HasPrefix(key, prefix) {
			out = append(out, copyKV(kv))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *MemoryStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire()
	m.del(key)
	return nil
}

func (m *MemoryStore) Create(_ context.Context, key string, value []byte, lease LeaseID) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire()
	if kv, ok := m.kvs[key]; ok {
		return kv.Revision, false, nil
	}
	return m.put(key, value, lease), true, nil
}

func (m *MemoryStore) CompareAndSwap(_ context.Context, key string, value []byte, rev int64, lease LeaseID) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire()
	kv, ok := m.kvs[key]
	switch {
	case !ok && rev != 0:
		return 0, false, nil
	case ok && kv.Revision != rev:
		return kv.Revision, false, nil
	}
	return m.put(key, value, lease), true, nil
}

func (m *MemoryStore) Watch(ctx context.Context, prefix string) (<-chan Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrStoreClosed
	}
	w := &memWatcher{prefix: prefix, ch: make(chan Event, 64), done: make(chan struct{})}
	m.watchers = append(m.watchers, w)
	go func() {
		<-ctx.Done()
		m.removeWatcher(w)
	}()
	return w.ch, nil
}

func (m *MemoryStore) Close() error {
	m.mu.Lock()
	watchers := m.watchers
	m.watchers = nil
	m.closed = true
	m.mu.Unlock()
	for _, w := range watchers {
		close(w.done)
		close(w.ch)
	}
	return nil
}

func (m *MemoryStore) removeWatcher(w *memWatcher) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, other := range m.watchers {
		if other == w {
			m.watchers = append(m.watchers[:i], m.watchers[i+1:]...)
			close(w.done)
			close(w.ch)
			return
		}
	}
}

// expire drops keys whose lease deadline has passed, callers hold the lock.
func (m *MemoryStore) expire() {
	now := m.now()
	for id, lease := range m.leases {
		if now.Before(lease.deadline) {
			continue
		}
		delete(m.leases, id)
		m.dropLeaseKeys(id)
	}
}

func (m *MemoryStore) dropLeaseKeys(id LeaseID) {
	for key, lease := range m.keyLease {
		if lease == id {
			m.del(key)
		}
	}
}

func (m *MemoryStore) put(key string, value []byte, lease LeaseID) int64 {
	m.rev++
	kv := KV{Key: key, Value: append([]byte(nil), value...), Revision: m.rev}
	m.kvs[key] = kv
	if lease == NoLease {
		delete(m.keyLease, key)
	} else {
		m.keyLease[key] = lease
	}
	m.notify(Event{Type: EventPut, KV: copyKV(kv)})
	return kv.Revision
}

func (m *MemoryStore) del(key string) {
	kv, ok := m.kvs[key]
	if !ok {
		return
	}
	delete(m.kvs, key)
	delete(m.keyLease, key)
	m.rev++
	kv.Revision = m.rev
	m.notify(Event{Type: EventDelete, KV: copyKV(kv)})
}

func (m *MemoryStore) notify(ev Event) {
	for _, w := range m.watchers {
		if !strings.HasPrefix(ev.KV.Key, w.prefix) {
			continue
		}
		select {
		case w.ch <- ev:
		case <-w.done:
		default: // slow watcher, the periodic refresh will catch up
		}
	}
}

func copyKV(kv KV) KV {
	kv.Value = append([]byte(nil), kv.Value...)
	return kv
}
