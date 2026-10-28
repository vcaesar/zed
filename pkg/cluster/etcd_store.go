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
	"math"
	"time"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	client "go.etcd.io/etcd/client/v3"

	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/errors"
)

// EtcdStore is the production Store, it maps our small interface onto the
// etcd v3 client. Revisions are etcd mod revisions, which is what makes them
// usable as fencing tokens.
type EtcdStore struct {
	cli     *client.Client
	timeout time.Duration
}

// NewEtcdStore dials the etcd cluster configured in the [Etcd] section.
func NewEtcdStore() (*EtcdStore, error) {
	cli, err := client.New(client.Config{
		Endpoints:   config.Global.Etcd.Endpoints,
		DialTimeout: 5 * time.Second,
		Username:    config.Global.Etcd.Username,
		Password:    config.Global.Etcd.Password,
	})
	if err != nil {
		return nil, err
	}
	return &EtcdStore{cli: cli, timeout: 10 * time.Second}, nil
}

func (s *EtcdStore) ctx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.timeout)
}

func (s *EtcdStore) Grant(ctx context.Context, ttl time.Duration) (LeaseID, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	// etcd leases have second granularity, never round down to zero.
	seconds := int64(math.Ceil(ttl.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	resp, err := s.cli.Grant(ctx, seconds)
	if err != nil {
		return NoLease, err
	}
	return LeaseID(resp.ID), nil
}

func (s *EtcdStore) KeepAlive(ctx context.Context, id LeaseID) error {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	_, err := s.cli.KeepAliveOnce(ctx, client.LeaseID(id))
	if errors.Is(err, rpctypes.ErrLeaseNotFound) {
		// expired lease: report it like MemoryStore so callers re-join
		return errors.ErrKeyNotFound
	}
	return err
}

func (s *EtcdStore) Revoke(ctx context.Context, id LeaseID) error {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	_, err := s.cli.Revoke(ctx, client.LeaseID(id))
	if errors.Is(err, rpctypes.ErrLeaseNotFound) {
		return nil // already expired, like MemoryStore
	}
	return err
}

func (s *EtcdStore) Put(ctx context.Context, key string, value []byte, lease LeaseID) (int64, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	resp, err := s.cli.Put(ctx, key, string(value), leaseOpts(lease)...)
	if err != nil {
		return 0, err
	}
	return resp.Header.Revision, nil
}

func (s *EtcdStore) Get(ctx context.Context, key string) (KV, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	resp, err := s.cli.Get(ctx, key)
	if err != nil {
		return KV{}, err
	}
	if len(resp.Kvs) == 0 {
		return KV{}, errors.ErrKeyNotFound
	}
	kv := resp.Kvs[0]
	return KV{Key: string(kv.Key), Value: append([]byte(nil), kv.Value...), Revision: kv.ModRevision}, nil
}

func (s *EtcdStore) List(ctx context.Context, prefix string) ([]KV, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	resp, err := s.cli.Get(ctx, prefix, client.WithPrefix(), client.WithSort(client.SortByKey, client.SortAscend))
	if err != nil {
		return nil, err
	}
	out := make([]KV, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		out = append(out, KV{Key: string(kv.Key), Value: append([]byte(nil), kv.Value...), Revision: kv.ModRevision})
	}
	return out, nil
}

func (s *EtcdStore) Delete(ctx context.Context, key string) error {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	_, err := s.cli.Delete(ctx, key)
	return err
}

func (s *EtcdStore) Create(ctx context.Context, key string, value []byte, lease LeaseID) (int64, bool, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	resp, err := s.cli.Txn(ctx).
		If(client.Compare(client.CreateRevision(key), "=", 0)).
		Then(client.OpPut(key, string(value), leaseOpts(lease)...)).
		Else(client.OpGet(key)).
		Commit()
	if err != nil {
		return 0, false, err
	}
	if resp.Succeeded {
		return resp.Header.Revision, true, nil
	}
	return existingRevision(resp), false, nil
}

func (s *EtcdStore) CompareAndSwap(ctx context.Context, key string, value []byte, rev int64, lease LeaseID) (int64, bool, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	resp, err := s.cli.Txn(ctx).
		If(client.Compare(client.ModRevision(key), "=", rev)).
		Then(client.OpPut(key, string(value), leaseOpts(lease)...)).
		Else(client.OpGet(key)).
		Commit()
	if err != nil {
		return 0, false, err
	}
	if resp.Succeeded {
		return resp.Header.Revision, true, nil
	}
	return existingRevision(resp), false, nil
}

func (s *EtcdStore) Watch(ctx context.Context, prefix string) (<-chan Event, error) {
	watchCh := s.cli.Watch(ctx, prefix, client.WithPrefix())
	out := make(chan Event, 64)
	go func() {
		defer close(out)
		for resp := range watchCh {
			for _, ev := range resp.Events {
				event := Event{
					Type: EventPut,
					KV: KV{
						Key:      string(ev.Kv.Key),
						Value:    append([]byte(nil), ev.Kv.Value...),
						Revision: ev.Kv.ModRevision,
					},
				}
				if ev.Type == client.EventTypeDelete {
					event.Type = EventDelete
				}
				select {
				case out <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (s *EtcdStore) Close() error {
	return s.cli.Close()
}

func leaseOpts(lease LeaseID) []client.OpOption {
	if lease == NoLease {
		return nil
	}
	return []client.OpOption{client.WithLease(client.LeaseID(lease))}
}

func existingRevision(resp *client.TxnResponse) int64 {
	for _, r := range resp.Responses {
		if get := r.GetResponseRange(); get != nil && len(get.Kvs) > 0 {
			return get.Kvs[0].ModRevision
		}
	}
	return 0
}
