/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"github.com/prometheus/client_golang/prometheus"

	"sigs.k8s.io/node-readiness-controller/internal/info"
)

// registry is a dedicated Prometheus registry for the reporter.
var registry = prometheus.NewRegistry()

var (
	// reporterBuildInfo tracks the reporter binary version to track fleet version skew.
	reporterBuildInfo = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "node_readiness_reporter_build_info",
			Help: "Reporter binary version.",
			ConstLabels: prometheus.Labels{
				"version": info.GetVersion(),
			},
		},
		func() float64 { return 1 },
	)

	// reporterCheckDuration tracks the duration of health probe checks.
	reporterCheckDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "node_readiness_reporter_check_duration_seconds",
			Help:    "Duration of health probe checks.",
			Buckets: []float64{0.005, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}, // 5ms to 10s
		},
	)

	// reporterChecksTotal tracks total probe check results over time.
	reporterChecksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "node_readiness_reporter_checks_total",
			Help: "Total probe check results over time.",
		},
		[]string{"result"}, // result: healthy, unhealthy, error
	)

	// reporterConditionWritesTotal tracks how the reporter handles Node condition updates.
	reporterConditionWritesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "node_readiness_reporter_condition_writes_total",
			Help: "Total Node condition updates handled by the reporter.",
		},
		[]string{"result"}, // result: success, error, skipped
	)
)

func init() {
	// Register custom metrics with the dedicated reporter registry.
	registry.MustRegister(reporterBuildInfo)
	registry.MustRegister(reporterCheckDuration)
	registry.MustRegister(reporterChecksTotal)
	registry.MustRegister(reporterConditionWritesTotal)
}
