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
	"errors"
	"time"
)

var (
	// ErrStoreClosed is returned by a store that has already been closed.
	ErrStoreClosed = errors.New("cluster: store closed")
	// ErrNotEnabled is returned by cluster calls made while clustering is off.
	ErrNotEnabled = errors.New("cluster: not enabled")
)

// LeaseID identifies a lease held by a node. NoLease means the key is
// written without a lease and therefore never expires on its own.
type LeaseID int64

// NoLease is the zero lease, keys written with it are permanent.
const NoLease LeaseID = 0

// KV is a single key with the store revision of its last write. The
// revision is the fencing token: it only ever grows for a given key.
type KV struct {
	Key      string
	Value    []byte
	Revision int64
}

// EventType tells apart a write from a removal in a Watch stream.
type EventType int

const (
	// EventPut is emitted when a key is created or updated.
	EventPut EventType = iota
	// EventDelete is emitted when a key is deleted or its lease expires.
	EventDelete
)

// Event is a single change observed on a watched prefix.
type Event struct {
	Type EventType
	KV   KV
}

// Store is the small slice of a consistent key value store the cluster
// needs: leases for liveness, revisions for fencing, prefix watches for
// change notification. It is implemented by etcd in production and by an
// in-memory store in tests.
type Store interface {
	// Grant creates a lease that expires after ttl unless kept alive.
	Grant(ctx context.Context, ttl time.Duration) (LeaseID, error)
	// KeepAlive refreshes a lease once, it must be called more often than the ttl.
	KeepAlive(ctx context.Context, id LeaseID) error
	// Revoke drops a lease and every key attached to it.
	Revoke(ctx context.Context, id LeaseID) error

	// Put writes key unconditionally and returns the new revision.
	Put(ctx context.Context, key string, value []byte, lease LeaseID) (int64, error)
	// Get returns a single key, errors.ErrKeyNotFound when it is absent.
	Get(ctx context.Context, key string) (KV, error)
	// List returns every key under prefix, sorted by key.
	List(ctx context.Context, prefix string) ([]KV, error)
	// Delete removes a key, removing a missing key is not an error.
	Delete(ctx context.Context, key string) error

	// Create writes key only when it does not exist yet. It reports whether
	// the write happened; when it did not, the returned revision belongs to
	// the value that is already there.
	Create(ctx context.Context, key string, value []byte, lease LeaseID) (int64, bool, error)
	// CompareAndSwap writes key only when its current revision is rev. Use
	// rev == 0 to require that the key does not exist.
	CompareAndSwap(ctx context.Context, key string, value []byte, rev int64, lease LeaseID) (int64, bool, error)

	// Watch streams changes under prefix until ctx is done.
	Watch(ctx context.Context, prefix string) (<-chan Event, error)

	// Close releases the underlying connection.
	Close() error
}
