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
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vcaesar/riot"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/logs"
	"github.com/vcaesar/zedsearch/pkg/meta"
	zedanalysis "github.com/vcaesar/zedsearch/pkg/uquery/analysis"
	"github.com/vcaesar/zedsearch/pkg/zutils"
	"github.com/vcaesar/zedsearch/pkg/zutils/flatten"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// BuildBlugeDocumentFromJSON returns the bluge document for the json document. It also updates the mapping for the fields if not found.
// If no mappings are found, it creates te mapping for all the encountered fields. If mapping for some fields is found but not for others
// then it creates the mapping for the missing fields.
func (s *IndexShard) BuildBlugeDocumentFromJSON(docID string, doc map[string]interface{}) (*riot.Document, error) {
	// Pick the index mapping from the cache if it already exists
	mappings := s.root.GetMappings()

	delete(doc, meta.ActionFieldName)
	delete(doc, meta.IDFieldName)
	delete(doc, meta.ShardFieldName)

	// Create a new bluge document
	bdoc := riot.NewDocument(docID)
	// Iterate through each field and add it to the bluge document
	for key, value := range doc {
		if value == nil || key == meta.TimeFieldName || key == meta.SourceFieldName {
			continue
		}

		prop, ok := mappings.GetProperty(key)
		if !ok || !prop.Index {
			continue // not index, skip
		}

		if prop.Type == "nested" || prop.Type == "join" {
			continue // expanded into hidden fields before write, nothing to index
		}

		if prop.Type == "vector" {
			if err := s.buildField(mappings, bdoc, key, value); err != nil {
				return nil, err
			}
			continue // whole array is one value
		}

		switch v := value.(type) {
		case []interface{}:
			for _, v := range v {
				if err := s.buildField(mappings, bdoc, key, v); err != nil {
					return nil, err
				}
			}
		default:
			if err := s.buildField(mappings, bdoc, key, v); err != nil {
				return nil, err
			}
		}
	}

	// set timestamp
	timestamp := time.Now()
	if value, ok := doc[meta.TimeFieldName]; ok {
		delete(doc, meta.TimeFieldName)
		switch value := value.(type) {
		case string:
			var err error
			timestamp, err = time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return nil, fmt.Errorf("invalid WAL timestamp: %w", err)
			}
		case float64:
			timestamp = time.Unix(0, int64(value))
		default:
			return nil, fmt.Errorf("invalid WAL timestamp type %T", value)
		}
	}
	bdoc.AddField(riot.NewDateTimeField(meta.TimeFieldName, timestamp).StoreValue().Sortable().Aggregatable())

	// set source
	var sourceByteVal []byte
	if v, ok := doc[meta.SourceFieldName]; ok && v != nil {
		sourceByteVal, _ = json.Marshal(v)
	} else {
		delete(doc, meta.SourceFieldName)
		sourceByteVal, _ = json.Marshal(doc)
	}
	bdoc.AddField(riot.NewStoredOnlyField("_source", sourceByteVal))

	bdoc.AddField(riot.NewStoredOnlyField("_index", []byte(s.GetIndexName())))
	bdoc.AddField(riot.NewCompositeFieldExcluding("_all", []string{"_id", "_index", "_source", meta.TimeFieldName}))

	// Add time for index
	bdoc.SetTimestamp(timestamp.UnixNano())
	// Upate metadata
	s.SetTimestamp(timestamp.UnixNano())

	return bdoc, nil
}

func (s *IndexShard) buildField(mappings *meta.Mappings, bdoc *riot.Document, key string, value interface{}) error {
	var field *riot.TermField
	prop, _ := mappings.GetProperty(key)
	switch prop.Type {
	case "text":
		v := value.(string)
		if v == "" {
			return nil
		}
		field = riot.NewTextField(key, v).SearchTermPositions()
		fieldAnalyzer, _ := zedanalysis.QueryAnalyzerForField(s.root.GetAnalyzers(), mappings, key)
		if fieldAnalyzer != nil {
			field.WithAnalyzer(fieldAnalyzer)
		}
	case "numeric":
		field = riot.NewNumericField(key, value.(float64))
	case "keyword":
		v := value.(string)
		if v == "" {
			return nil
		}
		field = riot.NewKeywordField(key, v)
	case "bool":
		field = riot.NewKeywordField(key, strconv.FormatBool(value.(bool)))
	case "geo_point":
		p, err := meta.ParseGeoPoint(value)
		if err != nil {
			return fmt.Errorf("field [%s] value [%v] parse err: %s", key, value, err.Error())
		}
		field = riot.NewGeoPointField(key, p.Lon, p.Lat)
	case "vector":
		vec, err := meta.ParseVector(value, prop.Dims)
		if err != nil {
			return fmt.Errorf("field [%s] value [%v] parse err: %s", key, value, err.Error())
		}
		if field, err = riot.NewVectorField(key, vec); err != nil {
			return fmt.Errorf("field [%s] value [%v] parse err: %s", key, value, err.Error())
		}
		bdoc.AddField(field) // stored-only: no terms, doc values or highlights
		return nil
	case "date", "time":
		v, err := zutils.ParseTime(value, prop.Format, prop.TimeZone)
		if err != nil {
			return fmt.Errorf("field [%s] value [%v] parse err: %s", key, value, err.Error())
		}
		field = riot.NewDateTimeField(key, v)
	}
	if prop.Store || prop.Highlightable {
		field.StoreValue()
	}
	if prop.Highlightable {
		field.HighlightMatches()
	}
	if prop.Sortable {
		field.Sortable()
	}
	if prop.Aggregatable {
		field.Aggregatable()
	}
	bdoc.AddField(field)

	for propField := range prop.Fields {
		err := s.buildField(mappings, bdoc, key+"."+propField, value)
		if err != nil {
			return err
		}
	}

	return nil
}

// CheckDocument checks if the document is valid.
func (s *IndexShard) CheckDocument(docID string, doc map[string]interface{}, update bool, shard int64) ([]byte, error) {
	// Pick the index mapping from the cache if it already exists
	mappings := s.root.GetMappings()
	mappingsNeedsUpdate := false
	isLogs := s.root.GetIndexType() == meta.IndexTypeLogs
	timestamp := time.Now()
	if isLogs {
		var err error
		doc, timestamp, err = logs.Normalize(doc, mappings, timestamp)
		if err != nil {
			return nil, err
		}
	}

	flatDoc, _ := flatten.Flatten(doc, "")
	mergeGeoPoints(mappings, flatDoc)
	if err := rewriteNestedJoinFields(mappings, flatDoc); err != nil {
		return nil, err
	}
	// Iterate through each field and add it to the bluge document
	for key, value := range flatDoc {
		if value == nil || (isLogs && key == meta.TimeFieldName) {
			continue
		}

		if update := s.checkProperty(mappings, key, value); update {
			mappingsNeedsUpdate = true
		}

		prop, ok := mappings.GetProperty(key)
		if !ok || !prop.Index {
			continue // not index, skip
		}

		if prop.Type == "nested" || prop.Type == "join" {
			continue // expanded into hidden fields, value itself is not indexed
		}

		if prop.Type == "vector" {
			if err := s.checkField(mappings, flatDoc, key, value, 0, false); err != nil {
				return nil, err
			}
			continue // whole array is one value
		}

		switch v := value.(type) {
		case []interface{}:
			for i, v := range v {
				if err := s.checkField(mappings, flatDoc, key, v, i, true); err != nil {
					return nil, err
				}
			}
		default:
			if err := s.checkField(mappings, flatDoc, key, v, 0, false); err != nil {
				return nil, err
			}
		}
	}

	var err error
	if mappingsNeedsUpdate {
		if err = s.root.SetMappings(mappings); err != nil {
			return nil, err
		}
		if err = StoreIndex(s.root); err != nil {
			return nil, err
		}
	}

	// set timestamp
	if value, ok := flatDoc[meta.TimeFieldName]; ok && !isLogs {
		delete(doc, meta.TimeFieldName)
		prop, _ := mappings.GetProperty(meta.TimeFieldName)
		v, err := zutils.ParseTime(value, prop.Format, prop.TimeZone)
		if err != nil {
			return nil, fmt.Errorf("field [%s] value [%v] parse err: %s", meta.TimeFieldName, value, err.Error())
		}
		timestamp = v
	}

	// prepare for wal
	action := meta.ActionTypeInsert
	if update {
		action = meta.ActionTypeUpdate
	}
	flatDoc[meta.ActionFieldName] = action
	flatDoc[meta.IDFieldName] = docID
	flatDoc[meta.ShardFieldName] = shard
	flatDoc[meta.TimeFieldName] = timestamp.UnixNano()
	if isLogs {
		// Preserve event-time precision across JSON WAL decoding to float64.
		flatDoc[meta.TimeFieldName] = timestamp.Format(time.RFC3339Nano)
	}
	flatDoc[meta.SourceFieldName] = doc

	return json.Marshal(flatDoc)
}

// checkProperty returns if need update mappings
func (s *IndexShard) checkProperty(mappings *meta.Mappings, key string, value interface{}) bool {
	prop, ok := mappings.GetProperty(key)
	if ok {
		if config.Global.EnableTextKeywordMapping && prop.Type == "text" {
			if _, ok := mappings.GetProperty(key + ".keyword"); ok {
				return false
			}
		} else {
			return false
		}
	}

	// try to find the type of the value and use it to define default mapping
	switch v := value.(type) {
	case string:
		if layout, ok := isDateProperty(v); ok {
			prop = meta.NewProperty("date")
			prop.Format = layout
			mappings.SetProperty(key, prop)
		} else {
			newProp := meta.NewProperty("text")
			if config.Global.EnableTextKeywordMapping {
				p := meta.NewProperty("keyword")
				newProp.AddField("keyword", p)
				mappings.SetProperty(key+".keyword", p)
			}
			mappings.SetProperty(key, newProp)
		}
	case int, int64, float64:
		mappings.SetProperty(key, meta.NewProperty("numeric"))
	case bool:
		mappings.SetProperty(key, meta.NewProperty("bool"))
	case []interface{}:
		if v, ok := value.([]interface{}); ok {
			for _, vv := range v {
				switch val := vv.(type) {
				case string:
					if layout, ok := isDateProperty(val); ok {
						prop = meta.NewProperty("date")
						prop.Format = layout
						mappings.SetProperty(key, prop)
					} else {
						newProp := meta.NewProperty("text")
						if config.Global.EnableTextKeywordMapping {
							p := meta.NewProperty("keyword")
							newProp.AddField("keyword", p)
							mappings.SetProperty(key+".keyword", p)
						}
						mappings.SetProperty(key, newProp)
					}
				case float64:
					mappings.SetProperty(key, meta.NewProperty("numeric"))
				case bool:
					mappings.SetProperty(key, meta.NewProperty("bool"))
				}
				break
			}
		}
	}

	return true
}

func (s *IndexShard) checkField(mappings *meta.Mappings, data map[string]interface{}, key string, value interface{}, id int, array bool) error {
	var err error
	var v interface{}
	prop, _ := mappings.GetProperty(key)
	switch prop.Type {
	case "text":
		v, err = zutils.ToString(value)
		if err != nil {
			return fmt.Errorf("field [%s] was set type to [text] but the value [%v] can't convert to string", key, value)
		}
	case "numeric":
		v, err = zutils.ToFloat64(value)
		if err != nil {
			return fmt.Errorf("field [%s] was set type to [numeric] but the value [%v] can't convert to int", key, value)
		}
	case "keyword":
		v, err = zutils.ToString(value)
		if err != nil {
			return fmt.Errorf("field [%s] was set type to [keyword] but the value [%v] can't convert to string", key, value)
		}
	case "bool":
		v, err = zutils.ToBool(value)
		if err != nil {
			return fmt.Errorf("field [%s] was set type to [bool] but the value [%v] can't convert to boolean", key, value)
		}
	case "geo_point":
		v, err = meta.ParseGeoPoint(value)
		if err != nil {
			return fmt.Errorf("field [%s] was set type to [geo_point] but the value [%v] parse err: %s", key, value, err.Error())
		}
	case "vector":
		v, err = meta.ParseVector(value, prop.Dims)
		if err != nil {
			return fmt.Errorf("field [%s] was set type to [vector] but the value [%v] parse err: %s", key, value, err.Error())
		}
	case "date", "time":
		_, err := zutils.ParseTime(value, prop.Format, prop.TimeZone)
		if err != nil {
			return fmt.Errorf("field [%s] value [%v] parse err: %s", key, value, err.Error())
		}
		v = value
	}
	if array {
		sub := data[key].([]interface{})
		sub[id] = v
		data[key] = sub
	} else {
		data[key] = v
	}

	return nil
}

// mergeGeoPoints re-assembles geo_point objects that flatten split into
// "key.lat"/"key.lon" or "key.type"/"key.coordinates" back into flatDoc[key],
// and collapses a single [lon, lat] array so it isn't indexed element-wise.
func mergeGeoPoints(mappings *meta.Mappings, flatDoc map[string]interface{}) {
	mappings.RangeProperty(func(key string, prop meta.Property) bool {
		if prop.Type != "geo_point" {
			return true
		}
		if coords, ok := flatDoc[key+".coordinates"]; ok {
			flatDoc[key] = coords
			delete(flatDoc, key+".coordinates")
			delete(flatDoc, key+".type")
		} else if lat, ok := flatDoc[key+".lat"]; ok {
			if lon, ok := flatDoc[key+".lon"]; ok {
				flatDoc[key] = map[string]interface{}{"lat": lat, "lon": lon}
				delete(flatDoc, key+".lat")
				delete(flatDoc, key+".lon")
			}
		}
		if arr, ok := flatDoc[key].([]interface{}); ok {
			if p, err := meta.ParseGeoPoint(arr); err == nil {
				flatDoc[key] = p
			}
		}
		return true
	})
}

// nestedCountField holds number of hidden documents created for nested
// property, as "_nested_count.<path>" numeric field of root document.
const nestedCountField = "_nested_count"

// nestedChild is hidden document created from one element of nested array.
type nestedChild struct {
	id  string
	doc map[string]interface{}
}

// nestedPaths returns sorted names of properties mapped as "nested".
func nestedPaths(mappings *meta.Mappings) []string {
	var paths []string
	mappings.RangeProperty(func(field string, prop meta.Property) bool {
		if prop.Type == "nested" {
			paths = append(paths, field)
		}
		return true
	})
	sort.Strings(paths)
	return paths
}

// joinProperty returns join property of index. Like Elasticsearch, only one
// join field per index is supported, first one by name is used.
func joinProperty(mappings *meta.Mappings) (string, meta.Property, bool) {
	name := ""
	var joinProp meta.Property
	mappings.RangeProperty(func(field string, prop meta.Property) bool {
		if prop.Type == "join" && (name == "" || field < name) {
			name, joinProp = field, prop
		}
		return true
	})
	return name, joinProp, name != ""
}

// rewriteNestedJoinFields replaces nested arrays and join field of flat
// document by hidden fields. Nested values are dropped from root document,
// they are indexed as own hidden documents, see buildNestedChildren.
func rewriteNestedJoinFields(mappings *meta.Mappings, flatDoc map[string]interface{}) error {
	if _, ok := flatDoc[meta.NestedPathField]; ok {
		return nil // hidden nested document, fields are already expanded
	}

	for _, path := range nestedPaths(mappings) {
		count := 0
		prefix := path + "."
		for key := range flatDoc {
			if key != path && !strings.HasPrefix(key, prefix) {
				continue
			}
			if idx, ok := nestedElementIndex(strings.TrimPrefix(key, prefix)); ok && idx+1 > count {
				count = idx + 1
			}
			delete(flatDoc, key)
		}
		if count > 0 {
			flatDoc[nestedCountField+"."+path] = float64(count)
		}
	}

	return rewriteJoinField(mappings, flatDoc)
}

// nestedElementIndex returns index of nested array element from flat key
// suffix, eg "0.name" -> 0.
func nestedElementIndex(suffix string) (int, bool) {
	if i := strings.Index(suffix, "."); i >= 0 {
		suffix = suffix[:i]
	}
	idx, err := strconv.Atoi(suffix)
	if err != nil || idx < 0 {
		return 0, false
	}
	return idx, true
}

// rewriteJoinField validates join value of document and replaces it by
// hidden `_join_name` / `_join_parent` fields.
func rewriteJoinField(mappings *meta.Mappings, flatDoc map[string]interface{}) error {
	field, prop, ok := joinProperty(mappings)
	if !ok {
		return nil
	}

	name, parent := "", ""
	if v, ok := flatDoc[field]; ok {
		s, err := zutils.ToString(v)
		if err != nil {
			return fmt.Errorf("field [%s] was set type to [join] but value [%v] can't convert to string", field, v)
		}
		name = s
	}
	if v, ok := flatDoc[field+".name"]; ok {
		s, err := zutils.ToString(v)
		if err != nil {
			return fmt.Errorf("field [%s] was set type to [join] but value [%v] can't convert to string", field+".name", v)
		}
		name = s
	}
	if v, ok := flatDoc[field+".parent"]; ok {
		s, err := zutils.ToString(v)
		if err != nil {
			return fmt.Errorf("field [%s] was set type to [join] but value [%v] can't convert to string", field+".parent", v)
		}
		parent = s
	}
	delete(flatDoc, field)
	delete(flatDoc, field+".name")
	delete(flatDoc, field+".parent")

	if name == "" {
		if parent != "" {
			return fmt.Errorf("field [%s] requires relation name for parent [%s]", field, parent)
		}
		return nil
	}

	isParent := false
	isChild := false
	for parentName, children := range prop.Relations {
		if parentName == name {
			isParent = true
		}
		for _, child := range children {
			if child == name {
				isChild = true
			}
		}
	}
	switch {
	case !isParent && !isChild:
		return fmt.Errorf("field [%s] has unknown relation [%s]", field, name)
	case isChild && parent == "":
		return fmt.Errorf("field [%s] relation [%s] requires parent id", field, name)
	case !isChild && parent != "":
		return fmt.Errorf("field [%s] relation [%s] is not child relation", field, name)
	}

	flatDoc[meta.JoinNameField] = name
	if parent != "" {
		flatDoc[meta.JoinParentField] = parent
	}

	return nil
}

// joinParentID returns id of parent document when doc is join child, so that
// children are routed to shard of their parent.
func joinParentID(mappings *meta.Mappings, doc map[string]interface{}) string {
	field, _, ok := joinProperty(mappings)
	if !ok {
		return ""
	}
	value, ok := doc[field].(map[string]interface{})
	if !ok {
		return ""
	}
	parent, err := zutils.ToString(value["parent"])
	if err != nil {
		return ""
	}
	return parent
}

// buildNestedChildren expands nested properties of document into hidden
// documents, one per element of nested array.
func buildNestedChildren(mappings *meta.Mappings, docID string, doc map[string]interface{}) ([]nestedChild, error) {
	paths := nestedPaths(mappings)
	if len(paths) == 0 || doc == nil {
		return nil, nil
	}

	var children []nestedChild
	for _, path := range paths {
		for i, element := range nestedElements(doc, path) {
			value, ok := element.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("field [%s] was set type to [nested] but element [%v] is not an object", path, element)
			}
			flatDoc, err := flatten.Flatten(map[string]interface{}{path: value}, "")
			if err != nil {
				return nil, fmt.Errorf("field [%s] element [%v] parse err: %s", path, element, err.Error())
			}
			flatDoc[meta.NestedPathField] = path
			flatDoc[meta.NestedParentField] = docID
			children = append(children, nestedChild{id: nestedChildID(docID, path, i), doc: flatDoc})
		}
	}

	return children, nil
}

// nestedChildID builds stable id of hidden document so that update and delete
// of root document can address its nested documents.
func nestedChildID(docID, path string, index int) string {
	return fmt.Sprintf("%s#%s#%d", docID, path, index)
}

// nestedElements returns elements of nested array at (dotted) path.
func nestedElements(doc map[string]interface{}, path string) []interface{} {
	var value interface{} = doc
	for _, key := range strings.Split(path, ".") {
		current, ok := value.(map[string]interface{})
		if !ok {
			return nil
		}
		value, ok = current[key]
		if !ok {
			return nil
		}
	}

	switch value := value.(type) {
	case []interface{}:
		return value
	case map[string]interface{}:
		return []interface{}{value}
	default:
		return nil
	}
}
