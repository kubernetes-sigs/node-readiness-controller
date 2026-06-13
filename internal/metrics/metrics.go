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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// RulesTotal tracks the number of NodeReadinessRules.
	RulesTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "node_readiness_rules_total",
			Help: "Number of NodeReadinessRules",
		},
	)

	// TaintOperations tracks the number of taint operations (add/remove).
	TaintOperations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "node_readiness_taint_operations_total",
			Help: "Total number of taint operations performed by the controller",
		},
		[]string{"rule", "operation"},
	)

	// EvaluationDuration tracks the duration of rule evaluations.
	EvaluationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "node_readiness_evaluation_duration_seconds",
			Help:    "Duration of rule evaluations",
			Buckets: prometheus.DefBuckets,
		},
	)

	// Failures tracks the number of operational failures.
	Failures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "node_readiness_failures_total",
			Help: "Total number of operational failures",
		},
		[]string{"rule", "reason"},
	)

	// BootstrapCompleted tracks the number of nodes that have completed bootstrap.
	BootstrapCompleted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "node_readiness_bootstrap_completed_total",
			Help: "Total number of nodes that have completed bootstrap",
		},
		[]string{"rule"},
	)

	// NodesByState tracks the number of nodes per rule per readiness state.
	NodesByState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "node_readiness_nodes_by_state",
			Help: "Number of nodes per rule broken down by readiness state (ready, not_ready, bootstrapping)",
		},
		[]string{"rule", "state"},
	)

	// ReconciliationLatency tracks end-to-end latency of taint operations per rule.
	ReconciliationLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "node_readiness_reconciliation_latency_seconds",
			Help:    "End-to-end latency of taint add/remove operations per rule",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"rule", "operation"},
	)

	// BootstrapDuration tracks time taken for a node to complete bootstrap per rule.
	BootstrapDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "node_readiness_bootstrap_duration_seconds",
			Help:    "Time taken for a node to complete bootstrap per rule",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"rule"},
	)
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(RulesTotal)
	metrics.Registry.MustRegister(TaintOperations)
	metrics.Registry.MustRegister(EvaluationDuration)
	metrics.Registry.MustRegister(Failures)
	metrics.Registry.MustRegister(BootstrapCompleted)
	metrics.Registry.MustRegister(NodesByState)
	metrics.Registry.MustRegister(ReconciliationLatency)
	metrics.Registry.MustRegister(BootstrapDuration)
}
