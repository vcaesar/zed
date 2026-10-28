/* Copyright 2022 Zinc Labs Inc. and Contributors
* Copyright 2026 AtomAI
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

package core

import (
	"strings"
	"time"

	"github.com/vcaesar/zedsearch/pkg/errors"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// CreateDocument inserts or updates a document in the zinc index
func (index *Index) CreateDocument(docID string, doc map[string]interface{}, update bool) error {
	if err := index.checkWritable(); err != nil {
		return err
	}

	// metrics
	IncrMetricStatsByIndex(index.GetName(), "wal_request")

	mappings := index.GetMappings()

	// check WAL
	shard := index.GetShardByDocID(routeDocID(mappings, docID, doc))
	if err := shard.OpenWAL(); err != nil {
		return err
	}

	secondShardID := ShardIDNeedLatest
	if update {
		secondShardID = ShardIDNeedUpdate
	}

	return index.writeDocument(shard, mappings, docID, doc, update, secondShardID)
}

// GetDocument get a document in the zinc index
func (index *Index) GetDocument(docID string) (*meta.Hit, error) {
	// check WAL
	shard := index.GetShardByDocID(docID)
	if err := shard.OpenWAL(); err != nil {
		return nil, err
	}

	hit, err := shard.FindDocumentByDocID(docID)
	if err == nil || err != errors.ErrorIDNotFound || !index.hasJoinField() {
		return hit, err
	}

	// join children are routed to shard of their parent
	for _, other := range index.shards {
		if other == shard {
			continue
		}
		if err := other.OpenWAL(); err != nil {
			return nil, err
		}
		if hit, err := other.FindDocumentByDocID(docID); err == nil {
			return hit, nil
		}
	}

	return nil, errors.ErrorIDNotFound
}

// UpdateDocument updates a document in the zinc index
func (index *Index) UpdateDocument(docID string, doc map[string]interface{}, insert bool) error {
	if err := index.checkWritable(); err != nil {
		return err
	}

	// metrics
	IncrMetricStatsByIndex(index.GetName(), "wal_request")

	mappings := index.GetMappings()

	// check WAL
	shard, secondShardID, err := index.findDocumentShard(docID)
	update := true
	if err != nil {
		if !insert || err != errors.ErrorIDNotFound {
			return err
		}
		update = false
		secondShardID = ShardIDNeedLatest
		shard = index.GetShardByDocID(routeDocID(mappings, docID, doc))
		if err := shard.OpenWAL(); err != nil {
			return err
		}
	}

	return index.writeDocument(shard, mappings, docID, doc, update, secondShardID)
}

// DeleteDocument deletes a document in the zinc index
func (index *Index) DeleteDocument(docID string) error {
	if err := index.checkWritable(); err != nil {
		return err
	}

	// metrics
	IncrMetricStatsByIndex(index.GetName(), "wal_request")

	// check WAL
	shard, secondShardID, err := index.findDocumentShard(docID)
	if err != nil {
		return err
	}

	mappings := index.GetMappings()
	stale, err := staleNestedChildren(shard, mappings, docID, nil)
	if err != nil {
		return err
	}

	if err := writeDeleteEntry(shard, docID, secondShardID); err != nil {
		return err
	}
	for _, childID := range stale {
		if err := writeDeleteEntry(shard, childID, ShardIDNeedUpdate); err != nil {
			return err
		}
	}

	return nil
}

// writeDocument writes document and hidden documents of its nested
// properties to WAL, removing hidden documents of previous version.
func (index *Index) writeDocument(shard *IndexShard, mappings *meta.Mappings, docID string, doc map[string]interface{}, update bool, secondShardID int64) error {
	children, err := buildNestedChildren(mappings, docID, doc)
	if err != nil {
		return err
	}

	// stale documents are looked up before write, WAL may be consumed in
	// background while document is written.
	var stale []string
	if update {
		if stale, err = staleNestedChildren(shard, mappings, docID, children); err != nil {
			return err
		}
	}

	data, err := shard.CheckDocument(docID, doc, update, secondShardID)
	if err != nil {
		return err
	}
	if err := shard.writeWAL(data); err != nil {
		return err
	}

	for _, child := range children {
		data, err := shard.CheckDocument(child.id, child.doc, update, secondShardID)
		if err != nil {
			return err
		}
		if err := shard.writeWAL(data); err != nil {
			return err
		}
	}

	for _, childID := range stale {
		if err := writeDeleteEntry(shard, childID, ShardIDNeedUpdate); err != nil {
			return err
		}
	}

	return nil
}

// staleNestedChildren returns ids of hidden documents of stored version of
// docID that are not rewritten by keep.
func staleNestedChildren(shard *IndexShard, mappings *meta.Mappings, docID string, keep []nestedChild) ([]string, error) {
	if len(nestedPaths(mappings)) == 0 {
		return nil, nil
	}

	hit, err := shard.FindDocumentByDocID(docID)
	if err != nil {
		if err == errors.ErrorIDNotFound {
			return nil, nil
		}
		return nil, err
	}
	source, ok := hit.Source.(map[string]interface{})
	if !ok {
		return nil, nil
	}

	old, err := buildNestedChildren(mappings, docID, source)
	if err != nil {
		return nil, err
	}

	keepIDs := make(map[string]struct{}, len(keep))
	for _, child := range keep {
		keepIDs[child.id] = struct{}{}
	}

	var stale []string
	for _, child := range old {
		if _, ok := keepIDs[child.id]; ok {
			continue
		}
		stale = append(stale, child.id)
	}

	return stale, nil
}

// writeDeleteEntry writes delete action for docID to WAL of shard.
func writeDeleteEntry(shard *IndexShard, docID string, secondShardID int64) error {
	data := map[string]interface{}{
		meta.IDFieldName:     docID,
		meta.ActionFieldName: meta.ActionTypeDelete,
		meta.ShardFieldName:  secondShardID,
	}
	jstr, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return shard.writeWAL(jstr)
}

// findDocumentShard returns first layer shard holding docID and id of its
// second layer shard. Join children are routed to shard of their parent, so
// hash of docID alone may point to wrong shard.
func (index *Index) findDocumentShard(docID string) (*IndexShard, int64, error) {
	shard := index.GetShardByDocID(docID)
	if err := shard.OpenWAL(); err != nil {
		return nil, ShardIDNeedLatest, err
	}

	secondShardID, err := shard.FindShardByDocID(docID)
	if err == nil {
		return shard, secondShardID, nil
	}
	if err != errors.ErrorIDNotFound || !index.hasJoinField() {
		return shard, secondShardID, err
	}

	for _, other := range index.shards {
		if other == shard {
			continue
		}
		if err := other.OpenWAL(); err != nil {
			return nil, ShardIDNeedLatest, err
		}
		if id, err := other.FindShardByDocID(docID); err == nil {
			return other, id, nil
		}
	}

	return shard, ShardIDNeedLatest, errors.ErrorIDNotFound
}

// hasJoinField reports whether index maps join property.
func (index *Index) hasJoinField() bool {
	_, ok := index.GetMappings().GetProperty(meta.JoinNameField)
	return ok
}

// routeDocID returns id used to pick shard of document: join children live on
// shard of their parent so that parent and children are written together.
func routeDocID(mappings *meta.Mappings, docID string, doc map[string]interface{}) string {
	if parent := joinParentID(mappings, doc); parent != "" {
		return parent
	}
	return docID
}

// isDateProperty returns true if the given value matches the default date format.
func isDateProperty(value string) (string, bool) {
	layout := detectTimeLayout(value)
	if layout == "" {
		return "", false
	}
	_, err := time.Parse(layout, value)
	return layout, err == nil
}

// detectTimeLayout tries to figure out the correct layout of the input date.
func detectTimeLayout(value string) string {
	layout := ""
	switch {
	case len(value) == 19 && strings.Index(value, " ") == 10:
		layout = "2006-01-02 15:04:05"
	case len(value) == 19 && strings.Index(value, "T") == 10:
		layout = "2006-01-02T15:04:05"
	case len(value) == 25 && strings.Index(value, "T") == 10:
		layout = time.RFC3339
	case len(value) == 29 && strings.Index(value, "T") == 10 && strings.Index(value, ".") == 19:
		layout = "2006-01-02T15:04:05.999Z07:00"
	}

	return layout
}
