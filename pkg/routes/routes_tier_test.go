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

package routes

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsDocumentWrite(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/api/:target/_doc/:id", want: true},
		{path: "/es/:target/_create/:id", want: true},
		{path: "/es/:target/_update/:id", want: true},
		{path: "/api/:target/_bulkv2", want: true},
		{path: "/api/:target/_multi", want: true},
		{path: "/es/:target/_delete_by_query", want: true},
		{path: "/es/:target/_search", want: false},
		{path: "/api/:target/_mapping", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, isDocumentWrite(tt.path))
		})
	}
}
