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

package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

func TestLogsIndexRequest(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		err  string
	}{
		{"logs only settings", `{"settings":{"index_type":"logs"}}`, ""},
		{"custom mapping", `{"settings":{"index_type":"logs"},"mappings":{"properties":{"message":{"type":"keyword"}}}}`, ""},
		{"unknown", `{"settings":{"index_type":"unknown"}}`, "index_type"},
		{"bad timestamp", `{"settings":{"index_type":"logs"},"mappings":{"properties":{"@timestamp":{"type":"keyword"}}}}`, "type [date]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var input map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(tt.body), &input))
			index, err := Request(input)
			if tt.err != "" {
				assert.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, index.Settings)
			assert.Equal(t, meta.IndexTypeLogs, index.Settings.IndexType)
		})
	}
}
