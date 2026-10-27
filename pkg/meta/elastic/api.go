/* Copyright 2022 Zinc Labs Inc. and Contributors
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

package elastic

import (
	"github.com/zincsearch/zincsearch/pkg/meta"
)

// CountResponse is the response of the ES compatible _count API.
type CountResponse struct {
	Count  int64       `json:"count"`
	Shards meta.Shards `json:"_shards"`
}

// ClusterHealthResponse is the response of the ES compatible _cluster/health API.
// ZincSearch is single node, so the cluster is always green with one node.
type ClusterHealthResponse struct {
	ClusterName                 string  `json:"cluster_name"`
	Status                      string  `json:"status"`
	TimedOut                    bool    `json:"timed_out"`
	NumberOfNodes               int     `json:"number_of_nodes"`
	NumberOfDataNodes           int     `json:"number_of_data_nodes"`
	ActivePrimaryShards         int64   `json:"active_primary_shards"`
	ActiveShards                int64   `json:"active_shards"`
	RelocatingShards            int     `json:"relocating_shards"`
	InitializingShards          int     `json:"initializing_shards"`
	UnassignedShards            int     `json:"unassigned_shards"`
	DelayedUnassignedShards     int     `json:"delayed_unassigned_shards"`
	NumberOfPendingTasks        int     `json:"number_of_pending_tasks"`
	NumberOfInFlightFetch       int     `json:"number_of_in_flight_fetch"`
	TaskMaxWaitingInQueueMillis int     `json:"task_max_waiting_in_queue_millis"`
	ActiveShardsPercentAsNumber float64 `json:"active_shards_percent_as_number"`
}

// CatIndexResponse is one row of the ES compatible _cat/indices API.
// All values are strings, like elasticsearch renders them.
type CatIndexResponse struct {
	Health       string `json:"health"`
	Status       string `json:"status"`
	Index        string `json:"index"`
	UUID         string `json:"uuid"`
	Pri          string `json:"pri"`
	Rep          string `json:"rep"`
	DocsCount    string `json:"docs.count"`
	DocsDeleted  string `json:"docs.deleted"`
	StoreSize    string `json:"store.size"`
	PriStoreSize string `json:"pri.store.size"`
}

// CatHealthResponse is one row of the ES compatible _cat/health API.
type CatHealthResponse struct {
	Epoch               string `json:"epoch"`
	Timestamp           string `json:"timestamp"`
	Cluster             string `json:"cluster"`
	Status              string `json:"status"`
	NodeTotal           string `json:"node.total"`
	NodeData            string `json:"node.data"`
	Shards              string `json:"shards"`
	Pri                 string `json:"pri"`
	Relo                string `json:"relo"`
	Init                string `json:"init"`
	Unassign            string `json:"unassign"`
	PendingTasks        string `json:"pending_tasks"`
	MaxTaskWaitTime     string `json:"max_task_wait_time"`
	ActiveShardsPercent string `json:"active_shards_percent"`
}

// FieldCapsResponse is the response of the ES compatible _field_caps API.
type FieldCapsResponse struct {
	Indices []string                       `json:"indices"`
	Fields  map[string]map[string]FieldCap `json:"fields"`
}

// FieldCap holds the capabilities of a single field for one field type.
type FieldCap struct {
	Type          string `json:"type"`
	MetadataField bool   `json:"metadata_field"`
	Searchable    bool   `json:"searchable"`
	Aggregatable  bool   `json:"aggregatable"`
}

// ReindexResponse is the response of the ES compatible _reindex API.
type ReindexResponse struct {
	Took                 int64                    `json:"took"`
	TimedOut             bool                     `json:"timed_out"`
	Total                int                      `json:"total"`
	Created              int                      `json:"created"`
	Updated              int                      `json:"updated"`
	Deleted              int                      `json:"deleted"`
	Batches              int                      `json:"batches"`
	VersionConflicts     int                      `json:"version_conflicts"`
	Noops                int                      `json:"noops"`
	Retries              meta.HttpRetriesResponse `json:"retries"`
	ThrottledMillis      int                      `json:"throttled_millis"`
	RequestsPerSecond    int                      `json:"requests_per_second"`
	ThrottledUntilMillis int                      `json:"throttled_until_millis"`
	Failures             []string                 `json:"failures"`
}
