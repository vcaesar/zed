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

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"github.com/zincsearch/zincsearch/pkg/cluster"
	"github.com/zincsearch/zincsearch/pkg/core"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/meta/elastic"
	"github.com/zincsearch/zincsearch/pkg/zutils"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

const (
	// ForwardedByHeader names the node that proxied a request, a request
	// carrying it must be served locally or fail, never proxied again.
	ForwardedByHeader = "X-Zinc-Forwarded-By"
	// ClusterEpochKey holds the ownership epoch the request was admitted
	// with, handlers may read it from the gin context.
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
func ClusterRoute() gin.HandlerFunc {
	return func(c *gin.Context) {
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
			forward(c, owner)
			return
		}

		c.Set(ClusterEpochKey, epoch)
		if !isMutating(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		serveFenced(c, target, epoch)
	}
}

// serveFenced runs the handler with its output buffered, so a handoff that
// happened while the write was in flight can still be reported to the
// client instead of a misleading success.
func serveFenced(c *gin.Context, target string, epoch int64) {
	buffer := &bufferedWriter{ResponseWriter: c.Writer}
	c.Writer = buffer
	c.Next()
	c.Writer = buffer.ResponseWriter

	err := cluster.CheckEpoch(target, epoch)
	if err == nil {
		if flushErr := buffer.flush(); flushErr != nil {
			log.Error().Err(flushErr).Msg("cluster: flush buffered response")
		}
		return
	}

	owner := ""
	var epochErr *cluster.EpochError
	if errors.As(err, &epochErr) {
		owner = epochErr.Owner
	}
	log.Warn().Err(err).Str("index", target).Str("owner", owner).
		Msg("cluster: rejecting write, partition moved")
	c.Abort()
	zutils.GinRenderJSON(c, http.StatusConflict, gin.H{"error": "partition moved", "owner": owner})
}

// forward proxies the request to the node that owns the index.
func forward(c *gin.Context, owner string) {
	if c.GetHeader(ForwardedByHeader) != "" {
		// the sender thought we owned the index and we think somebody else
		// does: bouncing it again would loop
		c.Abort()
		zutils.GinRenderJSON(c, http.StatusLoopDetected,
			gin.H{"error": "request already forwarded", "owner": owner})
		return
	}

	addr, ok := cluster.NodeAddr(owner)
	if !ok || addr == "" {
		c.Abort()
		zutils.GinRenderJSON(c, http.StatusServiceUnavailable,
			gin.H{"error": "owner node is unreachable", "owner": owner})
		return
	}
	target, err := url.Parse(addr)
	if err != nil {
		log.Error().Err(err).Str("addr", addr).Msg("cluster: bad advertise address")
		c.Abort()
		zutils.GinRenderJSON(c, http.StatusServiceUnavailable,
			gin.H{"error": "owner node has an invalid address", "owner": owner})
		return
	}

	authorization := c.GetHeader("Authorization")
	local := localNodeID()
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
	proxy.ServeHTTP(proxyWriter{ResponseWriter: c.Writer, Flusher: c.Writer}, c.Request)
	c.Abort()
}

// proxyWriter hides the CloseNotifier of gin's writer from the reverse
// proxy: it is the legacy cancellation signal, the request context covers
// the same ground and not every writer (httptest's) implements it.
type proxyWriter struct {
	http.ResponseWriter
	http.Flusher
}

// scatterSearch splits a multi index search by owner, runs the parts in
// parallel against `/api/_internal/search` and merges the responses.
func scatterSearch(c *gin.Context, target string) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.Abort()
		zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	if err := c.Request.Body.Close(); err != nil {
		log.Warn().Err(err).Msg("cluster: close request body")
	}

	query := &meta.ZincQuery{Size: 10}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, query); err != nil {
			c.Abort()
			zutils.GinRenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
			return
		}
	}

	groups := groupByOwner(resolveIndexes(target))
	if _, local := groups[localNodeID()]; len(groups) <= 1 && (local || len(groups) == 0) {
		// everything we would ask for is ours: let the normal handler do it
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
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

	group, ctx := errgroup.WithContext(c.Request.Context())
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
		zutils.GinRenderJSON(c, http.StatusBadGateway, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	c.Abort()
	zutils.GinRenderJSON(c, http.StatusOK, cluster.MergeSearch(responses, query))
}

// internalSearch asks one node for its part of a scattered search.
func internalSearch(ctx context.Context, owner string, indexes []string, query *meta.ZincQuery, authorization string) (*meta.SearchResponse, error) {
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

// groupByOwner maps index names onto the node that owns them, indexes
// nobody owns yet are served locally.
func groupByOwner(indexes []string) map[string][]string {
	local := localNodeID()
	groups := make(map[string][]string)
	for _, name := range indexes {
		owner, _, ok := cluster.Owner(name)
		if !ok {
			owner = local
		}
		groups[owner] = append(groups[owner], name)
	}
	return groups
}

// resolveIndexes expands a target into concrete index names using the
// index list of this node. It is complete only when the nodes share their
// index metadata (ZINC_METADATA_STORAGE=etcd).
func resolveIndexes(target string) []string {
	patterns := elastic.SplitPatterns(target)
	if len(patterns) == 1 && (patterns[0] == "_all" || patterns[0] == "*") {
		patterns = nil
	}
	names := make([]string, 0, core.ZINC_INDEX_LIST.Len())
	for _, index := range core.ZINC_INDEX_LIST.List() {
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
func isScatterTarget(c *gin.Context, target string) bool {
	if c.GetHeader(ForwardedByHeader) != "" {
		return false
	}
	if !strings.HasPrefix(c.Request.URL.Path, "/es/") ||
		!strings.HasSuffix(c.Request.URL.Path, "/_search") {
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

// bufferedWriter keeps the handler output in memory until the fencing
// check passed, so a rejected write never leaks a success body.
type bufferedWriter struct {
	gin.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *bufferedWriter) Write(data []byte) (int, error) {
	return w.body.Write(data)
}

func (w *bufferedWriter) WriteString(s string) (int, error) {
	return w.body.WriteString(s)
}

func (w *bufferedWriter) WriteHeader(status int) {
	w.status = status
}

func (w *bufferedWriter) WriteHeaderNow() {}

func (w *bufferedWriter) Written() bool {
	return false
}

func (w *bufferedWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *bufferedWriter) Size() int {
	return w.body.Len()
}

func (w *bufferedWriter) flush() error {
	if w.status != 0 {
		w.ResponseWriter.WriteHeader(w.status)
	}
	if w.body.Len() == 0 {
		return nil
	}
	_, err := w.ResponseWriter.Write(w.body.Bytes())
	return err
}
