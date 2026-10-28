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

package routes

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"github.com/vcaesar/zedsearch/pkg/cluster"
	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/meta/elastic"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
	"github.com/vcaesar/zedsearch/pkg/zutils"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

const (
	// ForwardedByHeader names the node that proxied a request, a request
	// carrying it must be served locally or fail, never proxied again.
	ForwardedByHeader = "X-Zed-Forwarded-By"
	// ClusterEpochKey holds the ownership epoch the request was admitted
	// with, handlers may read it from the request context.
	ClusterEpochKey = "cluster_epoch"
	// InternalSearchPath is the node to node search endpoint.
	InternalSearchPath = "/api/_internal/search"

	scatterParallelism = 8
	scatterTimeout     = 30 * time.Second
)

// clusterClient is shared so node to node calls reuse connections.
var clusterClient = &http.Client{Timeout: scatterTimeout}

// ClusterRoute routes a request for the `:target` index to the node that
// owns it.
//
//   - clustering disabled: nothing happens, this is the single node build
//   - another node owns the index: the request is proxied to it
//   - we own it: the ownership epoch is recorded and, for writes, checked
//     again after the handler ran. A partition that moved meanwhile turns
//     the response into 409 so the client retries against the new owner
//   - several indexes or a glob on an Elasticsearch search path: the
//     request is split per owner and the responses are merged
func ClusterRoute() zhttp.HandlerFunc {
	return func(c zhttp.Context) {
		if !cluster.Enabled() {
			c.Next()
			return
		}

		target := c.Param("target")
		if isScatterTarget(c, target) {
			scatterSearch(c, target)
			return
		}
		if target == "" {
			c.Next()
			return
		}

		owner, epoch, ok := cluster.Owner(target)
		if !ok {
			c.Next() // not assigned yet, serve locally like a single node
			return
		}
		if owner != localNodeID() {
			read := !isMutating(c.Method(), c.Path())
			if read && cluster.IsLocalReplica(target) && replicaReadable(target) {
				c.Next() // warm replica of a read-only object-storage index
				return
			}
			var fallbacks []string
			if read && replicaReadable(target) {
				fallbacks = cluster.Replicas(target)
			}
			forward(c, owner, fallbacks)
			return
		}

		c.Set(ClusterEpochKey, epoch)
		if !isMutating(c.Method(), c.Path()) {
			c.Next()
			return
		}
		serveFenced(c, target, epoch)
	}
}

// serveFenced runs the handler with its output buffered, so a handoff that
// happened while the write was in flight can still be reported to the
// client instead of a misleading success.
func serveFenced(c zhttp.Context, target string, epoch int64) {
	// a node that may have lost its lease must not reach the WAL at all
	if errors.Is(cluster.CheckEpoch(target, epoch), cluster.ErrNotLive) {
		rejectWrite(c, target, cluster.ErrNotLive)
		return
	}
	flush := c.NextBuffered()

	err := cluster.CheckEpoch(target, epoch)
	if err == nil {
		if flushErr := flush(); flushErr != nil {
			log.Error().Err(flushErr).Msg("cluster: flush buffered response")
		}
		return
	}
	rejectWrite(c, target, err)
}

// rejectWrite answers 409 so the client retries against the current owner.
func rejectWrite(c zhttp.Context, target string, err error) {
	owner := ""
	var epochErr *cluster.EpochError
	if errors.As(err, &epochErr) {
		owner = epochErr.Owner
	}
	log.Warn().Err(err).Str("index", target).Str("owner", owner).
		Msg("cluster: rejecting write, partition moved")
	c.Abort()
	zutils.RenderJSON(c, http.StatusConflict, zhttp.H{"error": "partition moved", "owner": owner})
}

// forward proxies the request to the node that owns the index. When the
// owner cannot be resolved (it died, the partition is not reassigned yet)
// the first resolvable fallback, a read replica, gets the request instead.
func forward(c zhttp.Context, owner string, fallbacks []string) {
	if c.GetHeader(ForwardedByHeader) != "" {
		// the sender thought we owned the index and we think somebody else
		// does: bouncing it again would loop
		c.Abort()
		zutils.RenderJSON(c, http.StatusLoopDetected,
			zhttp.H{"error": "request already forwarded", "owner": owner})
		return
	}

	local := localNodeID()
	addr, ok := cluster.NodeAddr(owner)
	for _, replica := range fallbacks {
		if ok && addr != "" {
			break
		}
		if replica != local {
			addr, ok = cluster.NodeAddr(replica)
		}
	}
	if !ok || addr == "" {
		c.Abort()
		zutils.RenderJSON(c, http.StatusServiceUnavailable,
			zhttp.H{"error": "owner node is unreachable", "owner": owner})
		return
	}
	target, err := url.Parse(addr)
	if err != nil {
		log.Error().Err(err).Str("addr", addr).Msg("cluster: bad advertise address")
		c.Abort()
		zutils.RenderJSON(c, http.StatusServiceUnavailable,
			zhttp.H{"error": "owner node has an invalid address", "owner": owner})
		return
	}

	authorization := c.GetHeader("Authorization")
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = target.Host
			pr.Out.Header.Set(ForwardedByHeader, local)
			if authorization != "" {
				pr.Out.Header.Set("Authorization", authorization)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Error().Err(err).Str("owner", owner).Msg("cluster: forward failed")
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	c.ServeHTTP(proxy)
	c.Abort()
}

// scatterSearch splits a multi index search by owner, runs the parts in
// parallel against `/api/_internal/search` and merges the responses.
func scatterSearch(c zhttp.Context, target string) {
	body, err := io.ReadAll(c.Body())
	if err != nil {
		c.Abort()
		zutils.RenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	query := &meta.ZedQuery{Size: 10}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, query); err != nil {
			c.Abort()
			zutils.RenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
			return
		}
	}

	groups := groupByOwner(resolveIndexes(target))
	if _, local := groups[localNodeID()]; len(groups) <= 1 && (local || len(groups) == 0) {
		// everything we would ask for is ours: let the normal handler do it
		c.SetBody(body)
		c.Next()
		return
	}

	// every owner returns the full window so the merge can re-sort it
	sub := *query
	sub.From = 0
	if query.From+query.Size > 0 {
		sub.Size = query.From + query.Size
	}

	authorization := c.GetHeader("Authorization")
	responses := make([]*meta.SearchResponse, len(groups))
	owners := make([]string, 0, len(groups))
	for owner := range groups {
		owners = append(owners, owner)
	}

	group, ctx := errgroup.WithContext(c.Context())
	group.SetLimit(scatterParallelism)
	var mu sync.Mutex
	for i, owner := range owners {
		i, owner := i, owner
		group.Go(func() error {
			resp, err := internalSearch(ctx, owner, groups[owner], &sub, authorization)
			if err != nil {
				return err
			}
			mu.Lock()
			responses[i] = resp
			mu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		log.Error().Err(err).Str("target", target).Msg("cluster: scatter search failed")
		c.Abort()
		zutils.RenderJSON(c, http.StatusBadGateway, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	c.Abort()
	zutils.RenderJSON(c, http.StatusOK, cluster.MergeSearch(responses, query))
}

// internalSearch asks one node for its part of a scattered search.
func internalSearch(ctx context.Context, owner string, indexes []string, query *meta.ZedQuery, authorization string) (*meta.SearchResponse, error) {
	addr, ok := cluster.NodeAddr(owner)
	if !ok || addr == "" {
		return nil, errors.New("cluster: node " + owner + " is unreachable")
	}
	payload, err := json.Marshal(map[string]interface{}{"indexes": indexes, "query": query})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, addr+InternalSearchPath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ForwardedByHeader, localNodeID())
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}

	resp, err := clusterClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("cluster: close internal search body")
		}
	}()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("cluster: node " + owner + " returned " + resp.Status + ": " + string(data))
	}
	return cluster.DecodeSearchResponse(data)
}

// groupByOwner maps index names onto the node that serves them: the owner,
// or this node when it is a read replica of a cold index. Indexes nobody
// owns yet are served locally.
func groupByOwner(indexes []string) map[string][]string {
	local := localNodeID()
	groups := make(map[string][]string)
	for _, name := range indexes {
		owner, _, ok := cluster.Owner(name)
		if !ok || (owner != local && cluster.IsLocalReplica(name) && replicaReadable(name)) {
			owner = local
		}
		groups[owner] = append(groups[owner], name)
	}
	return groups
}

// replicaReadable reports whether a node other than the owner may serve
// reads of an index. Only cold indexes qualify: they are read-only, live on
// object storage and are opened without a writer or WAL, so every node sees
// the same data. Hot and warm indexes are read through the owner's writer
// and local-disk indexes exist on the owner only. A variable for tests.
var replicaReadable = func(name string) bool {
	index, ok := core.GetIndex(name)
	return ok && index.IsCold()
}

// resolveIndexes expands a target into concrete index names using the
// index list of this node. It is complete only when the nodes share their
// index metadata (ZED_METADATA_STORAGE=etcd).
func resolveIndexes(target string) []string {
	patterns := elastic.SplitPatterns(target)
	if len(patterns) == 1 && (patterns[0] == "_all" || patterns[0] == "*") {
		patterns = nil
	}
	names := make([]string, 0, core.ZED_INDEX_LIST.Len())
	for _, index := range core.ZED_INDEX_LIST.List() {
		name := index.GetName()
		if elastic.MatchAnyPattern(name, patterns) {
			names = append(names, name)
		}
	}
	return names
}

// isScatterTarget reports whether the request asks several indexes at once
// on an Elasticsearch search path. `/api/:target/_search` speaks the v1
// query format and is left alone.
func isScatterTarget(c zhttp.Context, target string) bool {
	if c.GetHeader(ForwardedByHeader) != "" {
		return false
	}
	if !strings.HasPrefix(c.Path(), "/es/") ||
		!strings.HasSuffix(c.Path(), "/_search") {
		return false
	}
	return target == "" || isMultiTarget(target)
}

func isMultiTarget(target string) bool {
	return strings.ContainsAny(target, ",*") || target == "_all"
}

// readOnlyPostSuffixes are POST endpoints that only read, fencing them
// would turn a search during a handoff into a spurious 409.
var readOnlyPostSuffixes = []string{"/_search", "/_msearch", "/_count", "/_field_caps", "/_analyze"}

func isMutating(method, path string) bool {
	switch method {
	case http.MethodPost:
		for _, suffix := range readOnlyPostSuffixes {
			if strings.HasSuffix(path, suffix) {
				return false
			}
		}
		return true
	case http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func localNodeID() string {
	if node := cluster.Local(); node != nil {
		return node.ID
	}
	return ""
}
