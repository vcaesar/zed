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

package meta

import (
	"bytes"
	"sync"

	"github.com/vcaesar/riot"

	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

type Mappings struct {
	Properties map[string]Property `json:"properties,omitempty"`
	lock       sync.RWMutex
}

type Property struct {
	Type           string `json:"type"` // text, keyword, date, numeric, boolean, geo_point, vector, nested, join
	Analyzer       string `json:"analyzer,omitempty"`
	SearchAnalyzer string `json:"search_analyzer,omitempty"`
	Format         string `json:"format,omitempty"`    // date format yyyy-MM-dd HH:mm:ss || yyyy-MM-dd || epoch_millis
	TimeZone       string `json:"time_zone,omitempty"` // date format time_zone
	Dims           int    `json:"dims,omitempty"`      // vector dimension, 0 accepts any
	Index          bool   `json:"index"`
	Store          bool   `json:"store"`
	Sortable       bool   `json:"sortable"`
	Aggregatable   bool   `json:"aggregatable"`
	Highlightable  bool   `json:"highlightable"`
	// Fields allow the same string value to be indexed in multiple ways for different purposes,
	// such as one field for search and a multi-field for sorting and aggregations,
	// or the same string value analyzed by different analyzers.
	// If the Fields property is defined within a sub-field, it will be ignored.
	//
	// Currently, only "text" fields support the Fields parameter.
	Fields map[string]Property `json:"fields,omitempty"`
	// Properties holds the sub-fields of a "nested" property. Each element of
	// the nested array is indexed as its own hidden document tagged with
	// NestedPathField / NestedParentField so nested queries and aggregations
	// keep per-object semantics.
	Properties map[string]Property `json:"properties,omitempty"`
	// Relations defines parent -> children for a "join" property, e.g.
	// {"question": ["answer", "comment"]}. Documents set the field to
	// {"name": "answer", "parent": "<parent _id>"} or just "question".
	Relations map[string][]string `json:"relations,omitempty"`
	// IndexOptions enables HNSW approximate search on a vector field by default.
	IndexOptions *VectorIndexOptions `json:"index_options,omitempty"`
	// Similarity is the default knn metric of a vector field, cosine when empty.
	Similarity riot.Metric `json:"similarity,omitempty"`
}

// VectorIndexOptions are the HNSW graph parameters of a vector field. Zero
// values take riot's defaults. Persisted graphs are keyed by them.
type VectorIndexOptions struct {
	M              int `json:"m,omitempty"`
	EfConstruction int `json:"ef_construction,omitempty"`
}

// Hidden fields used for nested and join documents.
const (
	NestedPathField   = "_nested_path"   // keyword: nested property path, only on hidden nested docs
	NestedParentField = "_nested_parent" // keyword: _id of the root document
	JoinNameField     = "_join_name"     // keyword: relation name (parent or child type)
	JoinParentField   = "_join_parent"   // keyword: _id of the parent document
)

func NewMappings() *Mappings {
	return &Mappings{
		Properties: make(map[string]Property),
	}
}

func NewProperty(typ string) Property {
	p := Property{
		Type:           typ,
		Analyzer:       "",
		SearchAnalyzer: "",
		Format:         "",
		TimeZone:       "",
		Index:          true,
		Store:          false,
		Sortable:       true,
		Aggregatable:   true,
		Highlightable:  false,
		Fields:         make(map[string]Property),
	}
	if typ == "text" || typ == "vector" {
		p.Sortable = false
		p.Aggregatable = false
	}
	return p
}

// AddField adds the given field to the property.
func (p *Property) AddField(field string, value Property) {
	if p.Fields == nil {
		p.Fields = make(map[string]Property)
	}

	value.Fields = nil

	// explicitly allow field overwriting
	p.Fields[field] = value
}

// DeepClone returns a copy of the Property.
func (p *Property) DeepClone() Property {
	prop := NewProperty(p.Type)
	prop.Analyzer = p.Analyzer
	prop.SearchAnalyzer = p.SearchAnalyzer
	prop.Format = p.Format
	prop.TimeZone = p.TimeZone
	prop.Dims = p.Dims
	prop.Similarity = p.Similarity
	if p.IndexOptions != nil {
		opts := *p.IndexOptions
		prop.IndexOptions = &opts
	}
	prop.Index = p.Index
	prop.Store = p.Store
	prop.Sortable = p.Sortable
	prop.Aggregatable = p.Aggregatable
	prop.Highlightable = p.Highlightable

	if p.Fields != nil {
		for k, v := range p.Fields {
			prop.Fields[k] = v.DeepClone()
		}
	} else {
		prop.Fields = nil
	}
	if p.Properties != nil {
		prop.Properties = make(map[string]Property, len(p.Properties))
		for k, v := range p.Properties {
			prop.Properties[k] = v.DeepClone()
		}
	}
	if p.Relations != nil {
		prop.Relations = make(map[string][]string, len(p.Relations))
		for k, v := range p.Relations {
			prop.Relations[k] = append([]string(nil), v...)
		}
	}

	return prop
}

func (t *Mappings) Len() int {
	t.lock.RLock()
	n := len(t.Properties)
	t.lock.RUnlock()
	return n
}

func (t *Mappings) SetProperty(field string, prop Property) {
	t.lock.Lock()
	t.Properties[field] = prop
	t.lock.Unlock()
}

func (t *Mappings) GetProperty(field string) (Property, bool) {
	t.lock.RLock()
	prop, ok := t.Properties[field]
	t.lock.RUnlock()
	return prop, ok
}

func (t *Mappings) ListProperty() map[string]Property {
	m := make(map[string]Property)
	t.lock.RLock()
	for k, v := range t.Properties {
		m[k] = v
	}
	t.lock.RUnlock()
	return m
}

// RangeProperty calls fn for each property until fn returns false. Unlike
// ListProperty it does not copy the map; fn runs under the read lock and must
// not modify t.
func (t *Mappings) RangeProperty(fn func(field string, prop Property) bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()
	for k, v := range t.Properties {
		if !fn(k, v) {
			return
		}
	}
}

// DeepClone returns a full copy of the mapping.
func (t *Mappings) DeepClone() *Mappings {
	m := NewMappings()

	t.lock.RLock()
	defer t.lock.RUnlock()

	for k, v := range t.Properties {
		m.Properties[k] = v.DeepClone()
	}

	return m
}

func (t *Mappings) MarshalJSON() ([]byte, error) {
	t.lock.RLock()
	defer t.lock.RUnlock()
	b := bytes.NewBuffer(nil)
	b.WriteString(`{"properties":`)
	p, err := json.Marshal(t.Properties)
	if err != nil {
		return nil, err
	}
	b.Write(p)
	b.WriteByte('}')
	return b.Bytes(), nil
}
