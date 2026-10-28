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
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vcaesar/zedsearch/pkg/cluster"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta/elastic"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

const (
	// ClusterName is the single node cluster name reported to ES clients.
	ClusterName = "zedsearch"
	statusGreen = "green"
	statusOpen  = "open"
)

var catIndicesHeaders = []string{
	"health", "status", "index", "uuid", "pri", "rep",
	"docs.count", "docs.deleted", "store.size", "pri.store.size",
}

var catHealthHeaders = []string{
	"epoch", "timestamp", "cluster", "status", "node.total", "node.data",
	"shards", "pri", "relo", "init", "unassign", "pending_tasks",
	"max_task_wait_time", "active_shards_percent",
}

// CatIndices lists the indexes like the ES _cat/indices API
//
// @Id CatIndices
// @Summary List indexes for compatible ES
// @security BasicAuth
// @Tags    Cluster
// @Produce plain
// @Param   index  path   string  false  "Index"
// @Param   format query  string  false  "Response format, json or text"
// @Param   v      query  string  false  "Show the column header"
// @Success 200 {object} []elastic.CatIndexResponse
// @Router /es/_cat/indices [get]
func CatIndices(c *gin.Context) {
	patterns := elastic.SplitPatterns(c.Param("target"))

	indexes := make([]*core.Index, 0, core.ZINC_INDEX_LIST.Len())
	for _, index := range core.ZINC_INDEX_LIST.List() {
		if elastic.MatchAnyPattern(index.GetName(), patterns) {
			indexes = append(indexes, index)
		}
	}
	sort.Slice(indexes, func(i, j int) bool {
		return indexes[i].GetName() < indexes[j].GetName()
	})

	items := make([]elastic.CatIndexResponse, 0, len(indexes))
	for _, index := range indexes {
		stats := index.GetStats()
		var replicas int64
		if settings := index.GetSettings(); settings != nil {
			replicas = settings.NumberOfReplicas
		}
		items = append(items, elastic.CatIndexResponse{
			Health: statusGreen,
			Status: statusOpen,
			Index:  index.GetName(),
			// zinc has no index uuid, the name identifies the index
			UUID:         index.GetName(),
			Pri:          strconv.FormatInt(index.GetShardNum(), 10),
			Rep:          strconv.FormatInt(replicas, 10),
			DocsCount:    strconv.FormatUint(stats.DocNum, 10),
			DocsDeleted:  "0",
			StoreSize:    formatBytes(stats.StorageSize),
			PriStoreSize: formatBytes(stats.StorageSize),
		})
	}

	if isJSONFormat(c) {
		zutils.GinRenderJSON(c, http.StatusOK, items)
		return
	}

	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.Health, item.Status, item.Index, item.UUID, item.Pri, item.Rep,
			item.DocsCount, item.DocsDeleted, item.StoreSize, item.PriStoreSize,
		})
	}
	renderCatText(c, catIndicesHeaders, rows)
}

// CatHealth reports the cluster health like the ES _cat/health API
//
// @Id CatHealth
// @Summary Get cluster health for compatible ES
// @security BasicAuth
// @Tags    Cluster
// @Produce plain
// @Param   format query  string  false  "Response format, json or text"
// @Param   v      query  string  false  "Show the column header"
// @Success 200 {object} []elastic.CatHealthResponse
// @Router /es/_cat/health [get]
func CatHealth(c *gin.Context) {
	now := time.Now()
	shards := activeShards("")
	nodes := strconv.Itoa(len(cluster.Nodes()))
	item := elastic.CatHealthResponse{
		Epoch:               strconv.FormatInt(now.Unix(), 10),
		Timestamp:           now.UTC().Format("15:04:05"),
		Cluster:             clusterName(),
		Status:              statusGreen,
		NodeTotal:           nodes,
		NodeData:            nodes,
		Shards:              strconv.FormatInt(shards, 10),
		Pri:                 strconv.FormatInt(shards, 10),
		Relo:                "0",
		Init:                "0",
		Unassign:            "0",
		PendingTasks:        "0",
		MaxTaskWaitTime:     "-",
		ActiveShardsPercent: "100.0%",
	}

	if isJSONFormat(c) {
		zutils.GinRenderJSON(c, http.StatusOK, []elastic.CatHealthResponse{item})
		return
	}

	renderCatText(c, catHealthHeaders, [][]string{{
		item.Epoch, item.Timestamp, item.Cluster, item.Status, item.NodeTotal, item.NodeData,
		item.Shards, item.Pri, item.Relo, item.Init, item.Unassign, item.PendingTasks,
		item.MaxTaskWaitTime, item.ActiveShardsPercent,
	}})
}

func isJSONFormat(c *gin.Context) bool {
	return strings.EqualFold(c.Query("format"), "json")
}

// clusterName is the fixed single node name, unless clustering is on and
// the deployment picked its own name.
func clusterName() string {
	if cluster.Enabled() && config.Global.Cluster.Name != "" {
		return config.Global.Cluster.Name
	}
	return ClusterName
}

// renderCatText renders whitespace aligned columns, the header is only
// written when the request carries the `v` query parameter, like ES does.
func renderCatText(c *gin.Context, headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	_, withHeader := c.GetQuery("v")
	if withHeader {
		for i, header := range headers {
			widths[i] = len(header)
		}
	}
	for _, row := range rows {
		for i, value := range row {
			if len(value) > widths[i] {
				widths[i] = len(value)
			}
		}
	}

	buf := bytes.NewBuffer(nil)
	if withHeader {
		writeCatRow(buf, widths, headers)
	}
	for _, row := range rows {
		writeCatRow(buf, widths, row)
	}

	c.Data(http.StatusOK, "text/plain; charset=utf-8", buf.Bytes())
}

func writeCatRow(buf *bytes.Buffer, widths []int, row []string) {
	for i, value := range row {
		if i > 0 {
			buf.WriteByte(' ')
		}
		if i == len(row)-1 {
			buf.WriteString(value)
			break
		}
		buf.WriteString(value)
		for n := len(value); n < widths[i]; n++ {
			buf.WriteByte(' ')
		}
	}
	buf.WriteByte('\n')
}

// formatBytes renders a byte size the way elasticsearch does, eg. 1.2kb.
func formatBytes(size uint64) string {
	const unit = 1024
	if size < unit {
		return strconv.FormatUint(size, 10) + "b"
	}
	value := float64(size)
	units := []string{"kb", "mb", "gb", "tb", "pb"}
	for _, suffix := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f%s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f%s", value, units[len(units)-1])
}
