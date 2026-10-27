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
	"path"
	"strings"
)

var globEscaper = strings.NewReplacer(`\`, `\\`, `[`, `\[`)

// MatchPattern reports whether name matches the glob pattern.
// An empty pattern matches everything, an invalid pattern falls back to
// an exact comparison.
func MatchPattern(name, pattern string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") && !strings.Contains(pattern, "?") {
		return name == pattern
	}
	// only * and ? are wildcards, [ and \ are literal in index and field names
	ok, err := path.Match(globEscaper.Replace(pattern), name)
	if err != nil {
		return name == pattern
	}
	return ok
}

// MatchAnyPattern reports whether name matches at least one of the patterns.
// An empty pattern list matches everything.
func MatchAnyPattern(name string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		if MatchPattern(name, pattern) {
			return true
		}
	}
	return false
}

// SplitPatterns splits a comma separated list of patterns, dropping empty ones.
func SplitPatterns(s string) []string {
	patterns := make([]string, 0, 1)
	for _, v := range strings.Split(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			patterns = append(patterns, v)
		}
	}
	return patterns
}
