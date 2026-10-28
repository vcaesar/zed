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

// Package logs supplies the logs profile for ZedSearch's WAL-backed indexes.
package logs

import (
	"fmt"

	"github.com/vcaesar/zedsearch/pkg/meta"
)

// aliasFieldName is the OpenObserve-style event time input alias.
const aliasFieldName = "_timestamp"

var defaultFields = []struct{ name, typ string }{
	{meta.TimeFieldName, "date"},
	{"message", "text"},
	{"log", "text"},
	{"level", "keyword"},
	{"log.level", "keyword"},
	{"service.name", "keyword"},
	{"host.name", "keyword"},
	{"trace_id", "keyword"},
	{"span_id", "keyword"},
	{"tags", "keyword"},
}

// ApplyDefaults adds log search fields without replacing explicit mappings.
func ApplyDefaults(mappings *meta.Mappings) error {
	if err := ValidateMappings(mappings); err != nil {
		return err
	}
	for _, f := range defaultFields {
		if _, exists := mappings.GetProperty(f.name); !exists {
			mappings.SetProperty(f.name, meta.NewProperty(f.typ))
		}
	}
	// The input alias may contain strings or numbers. Search event time through
	// @timestamp, keeping the original alias only in _source by default.
	if _, exists := mappings.GetProperty(aliasFieldName); !exists {
		prop := meta.NewProperty("date")
		prop.Index, prop.Sortable, prop.Aggregatable = false, false, false
		mappings.SetProperty(aliasFieldName, prop)
	}
	return nil
}

func ValidateMappings(mappings *meta.Mappings) error {
	if prop, exists := mappings.GetProperty(meta.TimeFieldName); exists && prop.Type != "date" {
		return fmt.Errorf("logs index requires [%s] to have type [date]", meta.TimeFieldName)
	}
	return nil
}
