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

package meta

import (
	"fmt"
	"math"
	"strings"

	"github.com/vcaesar/riot"

	"github.com/vcaesar/zedsearch/pkg/zutils"
)

// vectorMetrics maps Elasticsearch/OpenSearch similarity names to riot metrics.
var vectorMetrics = map[string]riot.Metric{
	"cosine": riot.Cosine, "cosinesimil": riot.Cosine,
	"dot_product": riot.DotProduct, "dot": riot.DotProduct, "innerproduct": riot.DotProduct, "max_inner_product": riot.DotProduct,
	"l2_norm": riot.L2, "l2": riot.L2, "euclidean": riot.L2,
}

// ParseVectorMetric resolves an Elasticsearch similarity or OpenSearch
// space_type name to a riot metric.
func ParseVectorMetric(name string) (riot.Metric, error) {
	m, ok := vectorMetrics[strings.ToLower(name)]
	if !ok {
		return "", fmt.Errorf("unsupported similarity %q, expected cosine, dot_product or l2_norm", name)
	}
	return m, nil
}

// ParseVector accepts a vector field value as a numeric array and checks it
// against dims when dims > 0. All values must be finite.
func ParseVector(v interface{}, dims int) ([]float32, error) {
	var vec []float32
	switch v := v.(type) {
	case []float32:
		vec = v
	case []float64:
		vec = make([]float32, len(v))
		for i, f := range v {
			vec[i] = float32(f)
		}
	case []interface{}:
		vec = make([]float32, len(v))
		for i, e := range v {
			f, err := zutils.ToFloat64(e)
			if err != nil {
				return nil, fmt.Errorf("invalid vector element [%v] at %d, expected number", e, i)
			}
			vec[i] = float32(f)
		}
	default:
		return nil, fmt.Errorf("unsupported vector value type %T, expected numeric array", v)
	}
	if len(vec) == 0 {
		return nil, fmt.Errorf("vector must not be empty")
	}
	for i, f := range vec {
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			return nil, fmt.Errorf("vector element at %d is not finite", i)
		}
	}
	if dims > 0 && len(vec) != dims {
		return nil, fmt.Errorf("vector has %d dimensions, mapping expects %d", len(vec), dims)
	}
	return vec, nil
}
