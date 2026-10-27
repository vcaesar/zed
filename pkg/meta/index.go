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

package meta

import "fmt"

const (
	IndexTypeStandard = "standard"
	IndexTypeLogs     = "logs"
)

func CheckIndexType(indexType string) error {
	switch indexType {
	case "", IndexTypeStandard, IndexTypeLogs:
		return nil
	default:
		return fmt.Errorf("index_type [%s] is invalid, just accept [standard, logs]", indexType)
	}
}

type Index struct {
	ShardNum    int64                  `json:"shard_num"`
	Name        string                 `json:"name"`
	StorageType string                 `json:"storage_type"`
	Settings    *IndexSettings         `json:"settings,omitempty"`
	Mappings    *Mappings              `json:"mappings,omitempty"`
	Shards      map[string]*IndexShard `json:"shards"`
	Stats       IndexStat              `json:"stats"`
	Version     string                 `json:"version"`
}

type IndexShard struct {
	ShardNum int64               `json:"shard_num"`
	ID       string              `json:"id"`
	NodeID   string              `json:"node_id"` // remote instance ID
	Shards   []*IndexSecondShard `json:"shards"`
	Stats    IndexStat           `json:"stats"`
}

type IndexSecondShard struct {
	ID    int64     `json:"id"`
	Stats IndexStat `json:"stats"`
}

type IndexStat struct {
	DocTimeMin  int64  `json:"doc_time_min"`
	DocTimeMax  int64  `json:"doc_time_max"`
	DocNum      uint64 `json:"doc_num"`
	StorageSize uint64 `json:"storage_size"`
	WALSize     uint64 `json:"wal_size"`
}

type IndexSimple struct {
	Name        string                 `json:"name"`
	StorageType string                 `json:"storage_type"`
	ShardNum    int64                  `json:"shard_num"`
	Settings    *IndexSettings         `json:"settings,omitempty"`
	Mappings    map[string]interface{} `json:"mappings,omitempty"`
}

type IndexSettings struct {
	// IndexType selects the document profile; empty preserves standard indexes.
	IndexType        string         `json:"index_type,omitempty"`
	NumberOfShards   int64          `json:"number_of_shards,omitempty"`
	NumberOfReplicas int64          `json:"number_of_replicas,omitempty"`
	Analysis         *IndexAnalysis `json:"analysis,omitempty"`
	// Tier is the current data tier: hot (local disk, writable), warm
	// (object storage, writable) or cold (object storage, read-only,
	// shared by every node). Empty means hot.
	Tier string `json:"tier,omitempty"`
	// Lifecycle drives automatic tier transitions by index age.
	Lifecycle *IndexLifecycle `json:"lifecycle,omitempty"`
}

// IndexLifecycle mirrors a minimal ILM policy: move the index to the next
// tier once its newest document is older than the given duration ("7d", "30d").
type IndexLifecycle struct {
	Policy    string `json:"policy,omitempty"`
	WarmAfter string `json:"warm_after,omitempty"`
	ColdAfter string `json:"cold_after,omitempty"`
	// DeleteAfter removes the index entirely; empty keeps it forever.
	DeleteAfter string `json:"delete_after,omitempty"`
}

type IndexAnalysis struct {
	Analyzer    map[string]*Analyzer   `json:"analyzer,omitempty"`
	CharFilter  map[string]interface{} `json:"char_filter,omitempty"`
	Tokenizer   map[string]interface{} `json:"tokenizer,omitempty"`
	TokenFilter map[string]interface{} `json:"token_filter,omitempty"`
	Filter      map[string]interface{} `json:"filter,omitempty"` // compatibility with es, alias for TokenFilter
}
