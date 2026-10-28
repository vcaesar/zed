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
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
)

const metricsPath = "/metrics"

var (
	requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "zed",
		Name:      "requests_total",
		Help:      "How many HTTP requests processed, partitioned by status code and HTTP method.",
	}, []string{"code", "method", "url"})
	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: "zed",
		Name:      "request_duration_seconds",
		Help:      "The HTTP request latencies in seconds.",
	}, []string{"code", "method", "url"})
	responseSize = prometheus.NewSummary(prometheus.SummaryOpts{
		Subsystem: "zed",
		Name:      "response_size_bytes",
		Help:      "The HTTP response sizes in bytes.",
	})

	registerMetrics sync.Once
)

// SetPrometheus records request metrics and serves them on /metrics.
func SetPrometheus(app *zhttp.Engine) {
	if !config.Global.PrometheusEnable {
		return
	}

	registerMetrics.Do(func() {
		for _, c := range []prometheus.Collector{requestsTotal, requestDuration, responseSize, core.ZED_METRICS} {
			if err := prometheus.Register(c); err != nil {
				log.Error().Err(err).Msg("prometheus: register metric")
			}
		}
	})

	app.Use(func(c zhttp.Context) {
		if c.Path() == metricsPath {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()

		status := strconv.Itoa(c.ResponseStatus())
		requestDuration.WithLabelValues(status, c.Method(), c.Path()).Observe(time.Since(start).Seconds())
		requestsTotal.WithLabelValues(status, c.Method(), c.Path()).Inc()
		responseSize.Observe(float64(c.ResponseSize()))
	})

	metrics := promhttp.Handler()
	app.GET(metricsPath, func(c zhttp.Context) { c.ServeHTTP(metrics) })
}
