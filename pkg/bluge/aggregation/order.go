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

package aggregation

import (
	"strconv"
	"strings"

	"github.com/vcaesar/riot/search"
)

// Order keys understood by the terms style aggregations.
const (
	OrderByCount = "_count"
	OrderByKey   = "_key"
)

// bucketKeyLess orders two bucket keys. Numeric keys compare numerically, as
// elasticsearch orders the keys of a numeric field.
func bucketKeyLess(a, b *search.Bucket) bool {
	an, aerr := strconv.ParseFloat(a.Name(), 64)
	bn, berr := strconv.ParseFloat(b.Name(), 64)
	if aerr == nil && berr == nil {
		return an < bn
	}
	return a.Name() < b.Name()
}

// bucketMetricValue reads the value a sub-aggregation contributes to the
// bucket order. key is the sub-aggregation name, optionally suffixed with the
// metric to read, e.g "grades.avg". Unknown keys contribute 0, which keeps the
// buckets in their insertion order instead of failing the search.
func bucketMetricValue(b *search.Bucket, key string) float64 {
	name, metric := key, ""
	if b.Aggregation(name) == nil {
		if i := strings.LastIndex(key, "."); i > 0 {
			name, metric = key[:i], key[i+1:]
		}
	}
	switch calc := b.Aggregation(name).(type) {
	case *StatsCalculator:
		switch metric {
		case "min":
			return calc.Min()
		case "max":
			return calc.Max()
		case "sum":
			return calc.Sum()
		case "count":
			return float64(calc.Count())
		default:
			return calc.Avg()
		}
	case search.MetricCalculator:
		return calc.Value()
	default:
		return 0
	}
}

// orderLessFunc returns the ascending comparison for an order key.
func orderLessFunc(key string) func(a, b *search.Bucket) bool {
	switch key {
	case OrderByKey:
		return bucketKeyLess
	case OrderByCount:
		return func(a, b *search.Bucket) bool { return a.Count() < b.Count() }
	default:
		return func(a, b *search.Bucket) bool { return bucketMetricValue(a, key) < bucketMetricValue(b, key) }
	}
}
