# pkg/cluster

Multi-node ZedSearch: membership, index ownership, fenced handoff, request
routing and scatter-gather search. Coordination state lives in etcd, index
data does not.

## Model

Every index hashes to one of `zed_cluster_partitions` (256 by default)
partitions, and every partition is owned by exactly one node:

```
index name --fnv1a--> partition --etcd--> owner node
```

The owner is the only node that writes the index. Reads go to the owner too,
so a read always sees the owner's WAL.

The ownership map lives in etcd under
`<zed_etcd_prefix>/cluster/<zed_cluster_name>/`:

| key | meaning |
| --- | --- |
| `nodes/<id>` | member record, written under a lease, disappears when the node dies |
| `leader` | leader lock, created with a lease, whoever creates it rebalances |
| `assign/<partition>` | `{"owner": "<node id>"}`, its etcd mod revision is the **epoch** |
| `replicas` | `{"<partition>": ["<node id>", ...]}`, replicas, a separate key so a replica change never bumps an epoch |
| `insync` | `{"<index>": ["<node id>", ...]}`, replicas holding every acknowledged write (OpenSearch in-sync allocation ids) |

The epoch is never written by us: it is the store revision of the assignment
key, so it grows on every handoff and can be used as a fencing token.

## Fenced handoff

The hole in a plain "ownership map in etcd" design is the transfer window:
the new owner starts serving while the old owner is still finishing requests
it admitted before the move.

Every request that lands on the owner records the epoch it was admitted with
(`ClusterEpochKey` in the gin context). After a mutating handler returns, the
middleware checks the epoch again:

* unchanged: the buffered response is sent as is
* changed (or we are no longer the owner): `409 {"error":"partition moved","owner":"<id>"}`

Clients retry on 409; the retry is routed to the new owner.

This closes the window in which a stale owner could report success for a
write the cluster no longer counts as its own. It does **not** roll back a
WAL append that already happened - a write accepted before the handoff stays
in the old owner's WAL. Keep that in mind when a partition moves while a bulk
load is running.

## Request routing

`routes.ClusterRoute()` is installed before the API routes, so it covers
every `:target` route:

| case | behaviour |
| --- | --- |
| clustering disabled | nothing happens, single node build |
| partition unassigned | served locally |
| another node owns it | `httputil.ReverseProxy` to its advertise address, `Authorization` copied, `X-Zed-Forwarded-By: <node>` set |
| already forwarded, still not ours | `508 Loop Detected` |
| we own it, read | served locally |
| we own it, write | served locally, then the epoch is re-checked (see above) |
| we are a replica, read of a cold index | served locally, no hop |
| owner not resolvable, read of a cold index | proxied to the first live replica |
| `a,b`, `logs-*`, `_all` on `/es/.../_search` | split per owner, `POST /api/_internal/search` in parallel (at most 8 in flight), responses merged |

`/api/:target/_search` speaks the v1 query format and is never scattered.

Scatter-gather asks every owner, including this node, over HTTP. That costs
one loopback request but keeps one code path, and the internal endpoint never
scatters again, so there is no recursion.

Merging (`MergeSearch`):

* hits: re-sorted by score, or by the single sort field when the query has
  one, then cut to `from`/`size`
* `total` summed, `max_score` maximum, `took` the slowest node
* `count`/`sum`/`value_count` summed, `max`/`min` reduced, `avg` weighted by
  the `_partial {count, sum}` counters each node attaches
* bucket aggregations unioned by key, `doc_count` summed, sub-aggregations
  merged recursively
* everything else (`cardinality`, `percentiles`, `stats`, `top_hits`,
  pipelines) keeps the first node's value and logs a warning - those are
  approximations across nodes

## Replication (full HA)

`pkg/cluster/replication` follows the three OpenSearch replication paths.
Every write is acknowledged only after it is durable on more than one node
(or in the bucket):

| path | indexes | how | OpenSearch counterpart |
| --- | --- | --- | --- |
| `document` (default) | disk | the owner records every WAL entry with a sequence number and ships it to each replica before answering; replicas append it to their own WAL and index it | document replication, `ReplicationOperation` |
| `segment` | disk | entries are shipped the same way but replicas only keep them; every `zed_cluster_replication_interval` they copy the owner's segments (a checkpoint) and drop the entries it covers; on promotion they replay what they kept | segment replication, `FULL_REPLICATION` translog + `SegmentReplicationTarget` |
| `remote` | `s3`/`gcs`/`oss`, writable | entries are uploaded to `<index>/_translog/` before answering; segments already live in the bucket; a new owner replays the translog, a periodic flush persists the segments and trims it | remote-backed storage, `RemoteFsTranslog` |

`zed_cluster_replication` picks `document`, `segment` or `none` for disk
indexes; object-storage indexes always use the remote translog when
replication is on. Cold indexes take no writes and need none of this.

Write path on the owner (`serveFenced`):

1. `replication.Prepare` - the first write after a promotion replays the
   kept entries (segment) or the remote translog (remote)
2. the handler runs, `core.WALHook` records each entry in WAL order
3. `replication.Commit` ships everything recorded to every replica (or
   uploads it). A replica that fails is removed from the in-sync set
   *before* the write is acknowledged (OpenSearch `failShardIfNeeded`); if
   that removal cannot be written the write fails with `503`
4. the epoch is checked as before, then the response is sent

`_bulk` without an index in the path commits every index it touched.

The **term** is unique per owner incarnation (epoch + start time). A
replica on another term, or one missing entries the owner no longer
retains (100k entries), is asked to recover: it downloads a tar of the
owner's segments (`riot` point-in-time backup after the WAL was applied),
replaces its copy and continues from the snapshot's sequence number.
Entries are rewritten to idempotent updates, so an entry already in the
snapshot is harmless when replayed. A replica whose entries are retained is
caught up from the log without copying files.

**Promotion.** The leader only hands a replicated partition to a node in
the in-sync set of all its indexes. When the owner dies with no live
in-sync replica the partition stays on the dead owner (unavailable, like a
red shard) instead of going to a stale node. A rebalance never moves a
replicated partition off a live owner to a node that has not caught up.

**Reads.** Replicas serve reads of their copy once installed (near real
time, like OpenSearch replicas) and are the fallback when the owner is gone.

Node to node calls (`/api/_internal/replication/*`) carry
`X-Zed-Cluster-Secret`; set `zed_cluster_secret` to the same value on every
node. Replication refuses to start without it.

## Read replicas

`zed_cluster_replicas` (default 1) is the number of extra nodes the leader
assigns to each partition next to its owner. The start node rotates with
the partition, so one owner's replicas spread over all the other nodes.

Replicas serve reads of **cold** indexes (read-only on object storage,
every node sees the same data) and, with replication on, of **disk**
indexes once their copy is installed. Hot and warm object-storage indexes
are read through the owner's writer.

* a read of a cold index on one of its replicas is served locally
* a read whose owner cannot be resolved (died, not yet reassigned) is sent
  to the first live replica instead of failing with 503
* scatter-gather serves the indexes this node holds a ready replica of locally
* when an owner dies the rebalance hands its partitions to a live replica
  first (within quota), so failover lands on a node already serving them

Node addresses are cached from a membership watch (resynced every
heartbeat interval), so forwarding a request no longer reads etcd.

## Configuration

```toml
zed_cluster_enable = true
zed_cluster_name = "prod"
zed_cluster_node_id = ""                 # defaults to the hostname
zed_cluster_advertise_addr = "http://10.0.0.1:4080"
zed_cluster_partitions = 256
zed_cluster_heartbeat_interval = 5000000000   # 5s, in ns
zed_cluster_heartbeat_ttl = 15000000000       # 15s, in ns
zed_cluster_replicas = 1                      # replicas per partition
zed_cluster_replication = "document"          # document | segment | none
zed_cluster_replication_interval = 10000000000 # 10s: checkpoints, flushes, catch-up
zed_cluster_secret = "change-me"              # same on every node

zed_etcd_endpoints = ["http://10.0.0.10:2379"]
zed_etcd_prefix = "/zed"
```

Every setting is also an environment variable in upper case
(`ZED_CLUSTER_ENABLE`, `ZED_ETCD_ENDPOINTS`, ...).

### Three nodes with a shared etcd

```shell
# etcd (single node is fine for a test, run three for production)
docker run -d --name etcd -p 2379:2379 \
  -e ALLOW_NONE_AUTHENTICATION=yes \
  -e ETCD_ADVERTISE_CLIENT_URLS=http://10.0.0.10:2379 \
  -e ETCD_LISTEN_CLIENT_URLS=http://0.0.0.0:2379 \
  bitnami/etcd:latest

# node 1..3, same on every host with its own id and address
export ZED_FIRST_ADMIN_USER=admin ZED_FIRST_ADMIN_PASSWORD=Complexpass#123
export ZED_CLUSTER_ENABLE=true
export ZED_CLUSTER_NAME=prod
export ZED_ETCD_ENDPOINTS=http://10.0.0.10:2379
export ZED_CLUSTER_NODE_ID=node-1
export ZED_CLUSTER_ADVERTISE_ADDR=http://10.0.0.1:4080
export ZED_CLUSTER_SECRET=***
./zedsearch
```

Check the cluster:

```shell
curl -u admin:Complexpass#123 http://10.0.0.1:4080/api/cluster/nodes
curl -u admin:Complexpass#123 'http://10.0.0.1:4080/api/cluster/partitions?index=logs-2024'
curl -u admin:Complexpass#123 http://10.0.0.1:4080/es/_cat/health?v
```

## Guarantees

* one writer per index at any time, enforced by the ownership map
* handoffs are fenced by the partition epoch, a write admitted by the old
  owner cannot be reported as successful after the move
* leadership is only used to compute the assignment; every assignment write
  is a compare-and-swap, so two leaders during a partition cannot corrupt the
  map
* rebalancing is stable: adding or removing a node moves about
  `partitions / nodes` partitions, the rest stay where they are
* a node that dies loses its lease, its partitions are reassigned within
  `zed_cluster_heartbeat_ttl`
* indexes on object storage (`s3`, `gcs`, `oss`) are readable by whichever
  node takes over, so they survive node loss
* with replication on, an acknowledged write is on every in-sync replica
  (document, segment) or in the bucket (remote), and only an in-sync
  replica is promoted, so losing the owner loses no acknowledged write

## Limits

* replica state (term, sequence numbers, segment-mode kept entries) is in
  memory: a restarted replica copies the index from the owner again, and
  a restarted owner starts a new term, which makes its replicas recover
* recovery and segment checkpoints copy every segment of the index, not
  only the new ones; checkpoints are skipped when nothing changed
* with `zed_cluster_replicas = 0`, or `zed_cluster_replication = "none"`,
  disk indexes are back to failover by reassignment
* index metadata is shared through etcd: cluster mode always stores it
  there, whatever `ZED_METADATA_STORAGE` says, so every node sees every index
* the remote flush closes the writers of the index briefly, writes wait for
  it
* a 409 is not retried for the client, the caller has to repeat the request
* `cardinality`, `percentiles`, `stats` and `top_hits` are approximated in a
  scatter-gather search, the first responding owner wins
* an `avg` is weighted by the number of documents each node matched, which is
  exact only when the averaged field is present in every matched document

## Tests

```shell
go test ./pkg/cluster/...                    # unit tests, in-memory store and replication
ZED_ETCD_ENDPOINTS=http://127.0.0.1:2379 \
  go test ./pkg/cluster/ -run TestEtcdStoreIntegration   # against a real etcd
```
