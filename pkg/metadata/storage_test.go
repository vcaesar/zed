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

package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBackend(t *testing.T) {
	tests := []struct {
		name          string
		storage, mode string
		clusterEnable bool
		want          string
	}{
		{"default", "", "node", false, "bolt"},
		{"bolt", "bolt", "node", false, "bolt"},
		{"badger", "Badger", "node", false, "badger"},
		{"etcd", "etcd", "node", false, "etcd"},
		{"unknown falls back", "foo", "node", false, "bolt"},
		{"cluster enabled forces etcd", "bolt", "node", true, "etcd"},
		{"cluster server mode forces etcd", "badger", "Cluster", false, "etcd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, backend(tt.storage, tt.mode, tt.clusterEnable))
		})
	}
}
