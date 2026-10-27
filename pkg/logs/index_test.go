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

package logs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zincsearch/zincsearch/pkg/meta"
)

func TestApplyDefaults(t *testing.T) {
	mappings := meta.NewMappings()
	require.NoError(t, ApplyDefaults(mappings))
	for _, field := range []string{"message", "log"} {
		prop, ok := mappings.GetProperty(field)
		assert.True(t, ok)
		assert.Equal(t, "text", prop.Type)
		assert.True(t, prop.Index)
	}
	for _, field := range []string{"level", "log.level", "service.name", "host.name", "trace_id", "span_id", "tags"} {
		prop, ok := mappings.GetProperty(field)
		assert.True(t, ok)
		assert.Equal(t, "keyword", prop.Type)
		assert.True(t, prop.Aggregatable)
	}
	prop, ok := mappings.GetProperty(meta.TimeFieldName)
	assert.True(t, ok)
	assert.Equal(t, "date", prop.Type)
	assert.True(t, prop.Index && prop.Sortable && prop.Aggregatable)
	alias, ok := mappings.GetProperty("_timestamp")
	assert.True(t, ok)
	assert.False(t, alias.Index || alias.Sortable || alias.Aggregatable)

	message := meta.NewProperty("keyword")
	mappings.SetProperty("message", message)
	require.NoError(t, ApplyDefaults(mappings))
	got, _ := mappings.GetProperty("message")
	assert.Equal(t, message, got, "explicit mappings must win")

	mappings.SetProperty(meta.TimeFieldName, meta.NewProperty("text"))
	assert.ErrorContains(t, ApplyDefaults(mappings), "type [date]")
}
