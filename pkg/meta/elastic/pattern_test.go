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

package elastic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		pattern string
		want    bool
	}{
		{name: "empty pattern matches", value: "abc", pattern: "", want: true},
		{name: "star matches", value: "abc", pattern: "*", want: true},
		{name: "exact match", value: "abc", pattern: "abc", want: true},
		{name: "exact mismatch", value: "abc", pattern: "ab", want: false},
		{name: "prefix glob", value: "abc", pattern: "a*", want: true},
		{name: "suffix glob", value: "abc", pattern: "*bc", want: true},
		{name: "glob mismatch", value: "abc", pattern: "b*", want: false},
		{name: "question mark", value: "abc", pattern: "a?c", want: true},
		{name: "bracket is literal", value: "a[c", pattern: "a[c*", want: true},
		{name: "bracket is not a class", value: "logs1-x", pattern: "logs[1]-*", want: false},
		{name: "literal bracket name", value: "logs[1]-x", pattern: "logs[1]-*", want: true},
		{name: "backslash is literal", value: `a\b`, pattern: `a\*`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MatchPattern(tt.value, tt.pattern))
		})
	}
}

func TestMatchAnyPattern(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		patterns []string
		want     bool
	}{
		{name: "no patterns match everything", value: "abc", patterns: nil, want: true},
		{name: "one of them matches", value: "abc", patterns: []string{"x", "a*"}, want: true},
		{name: "none matches", value: "abc", patterns: []string{"x", "y*"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MatchAnyPattern(tt.value, tt.patterns))
		})
	}
}

func TestSplitPatterns(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "empty", value: "", want: []string{}},
		{name: "single", value: "abc", want: []string{"abc"}},
		{name: "multiple with spaces", value: "abc, def ,", want: []string{"abc", "def"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SplitPatterns(tt.value))
		})
	}
}
