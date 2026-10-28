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

package core

import (
	"github.com/prometheus/client_golang/prometheus"
)

// ZED_METRICS holds per index stats, exported once prometheus is enabled.
var ZED_METRICS = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Subsystem: "zed",
	Name:      "index_stats",
	Help:      "Summary index stats metric",
}, []string{"index", "field"})

func SetMetricStatsByIndex(index, field string, val float64) {
	ZED_METRICS.WithLabelValues(index, field).Set(val)
}

func IncrMetricStatsByIndex(index, field string) {
	ZED_METRICS.WithLabelValues(index, field).Inc()
}
