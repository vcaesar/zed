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

package mappings

import (
	"fmt"
	"strings"

	"github.com/vcaesar/riot/analysis"

	"github.com/zincsearch/zincsearch/pkg/config"
	"github.com/zincsearch/zincsearch/pkg/errors"
	"github.com/zincsearch/zincsearch/pkg/meta"
	zincanalysis "github.com/zincsearch/zincsearch/pkg/uquery/analysis"
	"github.com/zincsearch/zincsearch/pkg/zutils"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

func Request(analyzers map[string]*analysis.Analyzer, data map[string]interface{}) (*meta.Mappings, error) {
	if len(data) == 0 {
		return nil, nil
	}

	if sql, ok := data["sql"]; ok {
		if !config.Global.EnableSQLMappings {
			return nil, errors.New(errors.ErrorTypeParsingException, "[mappings] SQL mappings are disabled")
		}
		if len(data) != 1 {
			return nil, errors.New(errors.ErrorTypeParsingException, "[mappings] sql cannot be combined with other mapping options")
		}
		ddl, ok := sql.(string)
		if !ok || strings.TrimSpace(ddl) == "" {
			return nil, errors.New(errors.ErrorTypeParsingException, "[mappings] sql should be a non-empty string")
		}
		properties, err := sqlProperties(ddl)
		if err != nil {
			return nil, errors.New(errors.ErrorTypeParsingException, "[mappings] "+err.Error())
		}
		return Request(analyzers, map[string]interface{}{"properties": properties})
	}

	if data["properties"] == nil {
		return nil, errors.New(errors.ErrorTypeParsingException, "[mappings] properties should be defined")
	}

	properties, ok := data["properties"].(map[string]interface{})
	if !ok {
		return nil, errors.New(errors.ErrorTypeParsingException, "[mappings] properties should be an object")
	}

	mappings := meta.NewMappings()
	for field, prop := range properties {
		var propFields map[string]interface{}

		prop, ok := prop.(map[string]interface{})
		if !ok {
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] should be an object", field))
		}

		if v, ok := prop["properties"]; ok {
			if _, ok := v.(map[string]interface{}); !ok {
				return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] should be an object", field))
			}

			subMappings, err := Request(analyzers, prop)
			if err != nil {
				return nil, err
			}
			subProperties := subMappings.ListProperty()
			for k, v := range subProperties {
				mappings.SetProperty(field+"."+k, v)
			}

			// nested property keeps its sub-fields, every element of nested
			// array is indexed as own hidden document.
			if propType, ok := prop["type"].(string); ok && strings.ToLower(propType) == "nested" {
				nestedProp := newNestedProperty()
				nestedProp.Properties = subProperties
				mappings.SetProperty(field, nestedProp)
				setNestedFields(mappings)
			}

			continue
		}

		if v, ok := prop["fields"]; ok {
			if propFields, ok = v.(map[string]interface{}); !ok {
				return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] property.fields [%s] should be an object, got %T", field, v))
			}

			// check if sub field contains property "type"
			for _, subProp := range v.(map[string]any) {
				if subPropField, ok := subProp.(map[string]any); ok {
					subPropType, ok := subPropField["type"]
					if !ok {
						return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] should be exists", "type"))
					}
					if _, ok = subPropType.(string); !ok {
						return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] should be a string", "type"))
					}
				}
			}
		}

		propType, ok := prop["type"]
		if !ok {
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] should be exists", "type"))
		}

		propTypeStr, ok := propType.(string)
		if !ok {
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] should be a string", "type"))
		}

		var newProp meta.Property
		propTypeStr = strings.ToLower(propTypeStr)
		switch propTypeStr {
		case "text":
			newProp = meta.NewProperty(propTypeStr)

			if config.Global.EnableTextKeywordMapping {
				p := meta.NewProperty("keyword")
				newProp.AddField("keyword", p)
			}
		case "keyword", "numeric", "bool", "date":
			newProp = meta.NewProperty(propTypeStr)
		case "constant_keyword":
			newProp = meta.NewProperty("keyword")
		case "match_only_text":
			newProp = meta.NewProperty("text")
		case "integer", "double", "long", "short", "int", "float":
			newProp = meta.NewProperty("numeric")
		case "boolean":
			newProp = meta.NewProperty("bool")
		case "time", "datetime":
			newProp = meta.NewProperty("date")
		case "geo_point", "geo":
			newProp = meta.NewProperty("geo_point")
		case "vector", "dense_vector", "knn_vector":
			newProp = meta.NewProperty("vector")
		case "nested":
			// nested property without declared sub-fields, sub-fields of
			// nested documents are mapped dynamically.
			newProp = newNestedProperty()
			setNestedFields(mappings)
		case "join":
			newProp = meta.NewProperty("join")
			newProp.Sortable = false
			newProp.Aggregatable = false
			relations, err := parseRelations(field, prop["relations"])
			if err != nil {
				return nil, err
			}
			newProp.Relations = relations
			setJoinFields(mappings)
		case "flattened", "object", "wildcard", "byte", "alias", "ip", "ip_range", "scaled_float":
			// ignore
		default:
			return nil, errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[mappings] properties [%s] doesn't support type [%s]", field, propTypeStr))
		}

		for k, v := range prop {
			switch k {
			case "type":
				// handled
			case "analyzer":
				newProp.Analyzer = v.(string)
			case "search_analyzer":
				newProp.SearchAnalyzer = v.(string)
			case "format":
				newProp.Format = v.(string)
			case "time_zone":
				newProp.TimeZone = v.(string)
				_, err := zutils.ParseTimeZone(newProp.TimeZone)
				if err != nil {
					return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] %s time_zone parse err %s", field, err.Error()))
				}
			case "index":
				newProp.Index = v.(bool)
			case "dims", "dimension":
				dims, err := zutils.ToInt(v)
				if err != nil || dims <= 0 {
					return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] dims must be a positive integer", field))
				}
				newProp.Dims = dims
			case "store":
				newProp.Store = v.(bool)
			case "sortable":
				newProp.Sortable = v.(bool)
			case "aggregatable":
				newProp.Aggregatable = v.(bool)
			case "highlightable":
				newProp.Highlightable = v.(bool)
			default:
				// ignore unknown options
				// return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] unknown option [%s]", field, k))
			}
		}

		if newProp.Highlightable {
			newProp.Store = true
		}

		if newProp.Type == "vector" {
			// vector fields are stored-only: no terms, doc values or highlights
			newProp.Sortable = false
			newProp.Aggregatable = false
			newProp.Highlightable = false
		} else if newProp.Dims != 0 {
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] dims is only valid for type [vector]", field))
		}

		if newProp.Type != "" {
			mappings.SetProperty(field, newProp)
		}

		if newProp.Type == "text" {
			fields, err := convertToField(propFields)
			if err != nil {
				return nil, err
			}

			for k, v := range fields {
				newProp.AddField(k, v)
				mappings.SetProperty(field+"."+k, v)
			}

			// check analyzer
			if newProp.Analyzer != "" {
				if _, err := zincanalysis.QueryAnalyzer(analyzers, newProp.Analyzer); err != nil {
					return nil, err
				}
			}
			if newProp.SearchAnalyzer != "" {
				if _, err := zincanalysis.QueryAnalyzer(analyzers, newProp.SearchAnalyzer); err != nil {
					return nil, err
				}
			}
		}
	}

	return mappings, nil
}

// newNestedProperty returns property of "nested" type. Nested property holds
// no value of its own, every element of nested array is indexed as hidden
// document, so it is neither sortable nor aggregatable.
func newNestedProperty() meta.Property {
	prop := meta.NewProperty("nested")
	prop.Sortable = false
	prop.Aggregatable = false
	return prop
}

// setNestedFields registers hidden fields tagging nested documents.
func setNestedFields(mappings *meta.Mappings) {
	for _, field := range []string{meta.NestedPathField, meta.NestedParentField} {
		if _, ok := mappings.GetProperty(field); !ok {
			mappings.SetProperty(field, meta.NewProperty("keyword"))
		}
	}
}

// setJoinFields registers hidden fields tagging join documents.
func setJoinFields(mappings *meta.Mappings) {
	for _, field := range []string{meta.JoinNameField, meta.JoinParentField} {
		if _, ok := mappings.GetProperty(field); !ok {
			mappings.SetProperty(field, meta.NewProperty("keyword"))
		}
	}
}

// parseRelations parses relations of "join" property, mapping name of parent
// relation to names of its children: {"question": ["answer", "comment"]}.
func parseRelations(field string, v interface{}) (map[string][]string, error) {
	raw, ok := v.(map[string]interface{})
	if !ok {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] join requires [relations] object", field))
	}
	if len(raw) == 0 {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] join requires [relations] object", field))
	}

	relations := make(map[string][]string, len(raw))
	for parent, children := range raw {
		if parent == "" {
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] relations name should be non-empty", field))
		}
		var names []string
		switch children := children.(type) {
		case string:
			names = []string{children}
		case []interface{}:
			for _, child := range children {
				name, ok := child.(string)
				if !ok {
					return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] relations [%s] should be string or array of strings", field, parent))
				}
				names = append(names, name)
			}
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] relations [%s] should be string or array of strings", field, parent))
		}

		seen := make(map[string]struct{}, len(names))
		for _, name := range names {
			if name == "" || name == parent {
				return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] relations [%s] has invalid child [%s]", field, parent, name))
			}
			if _, ok := seen[name]; ok {
				return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[mappings] properties [%s] relations [%s] has duplicated child [%s]", field, parent, name))
			}
			seen[name] = struct{}{}
		}
		relations[parent] = names
	}

	return relations, nil
}

// convertToField converst v to type map[string]meta.Property.
func convertToField(v map[string]interface{}) (map[string]meta.Property, error) {
	r := make(map[string]meta.Property)

	if len(v) == 0 || v == nil {
		return r, nil
	}

	// it seems inefficient to encode and then directly decode it.
	// But in favor of code maintainability and to prevent that we need to maintain
	// duplicated code its more "efficient" to parse the fields this way.

	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}

	return r, nil
}
