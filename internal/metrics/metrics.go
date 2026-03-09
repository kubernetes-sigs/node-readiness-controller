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
	EvaluationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "node_readiness_evaluation_duration_seconds",
			Help:    "Duration of rule evaluations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"rule"},
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

	// ReconciliationLatency tracks end-to-end latency from condition change to taint operation.
	// This measures how quickly the controller responds to node condition changes.
	// Note: Uses in-memory tracking for condition transition times to avoid high cardinality.
	ReconciliationLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "node_readiness_reconciliation_latency_seconds",
			Help:    "End-to-end latency from node condition change to taint operation completion",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300}, // 10ms to 5min
		},
		[]string{"rule", "operation"}, // operation: add_taint, remove_taint
	)

	// BootstrapDuration tracks the time from node creation to bootstrap completion (taint removal).
	// This measures the end-to-end bootstrap time for nodes in bootstrap-only mode.
	// Bootstrap start time is tracked in-memory, not as a metric.
	BootstrapDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "node_readiness_bootstrap_duration_seconds",
			Help:    "Time from node creation to bootstrap completion (taint removal) for bootstrap-only rules",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600, 1200}, // 1s to 20min
		},
		[]string{"rule"},
	)

	// NodesByState tracks nodes in each readiness state per rule.
	// Provides a quick overview of cluster health.
	NodesByState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "node_readiness_nodes_by_state",
			Help: "Number of nodes in each readiness state per rule",
		},
		[]string{"rule", "state"}, // state: ready, not_ready, bootstrapping
	)

	// RuleLastReconciliationTime tracks when a rule was last reconciled.
	// This provides rule-level visibility for admins to detect stuck rules.
	RuleLastReconciliationTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "node_readiness_rule_last_reconciliation_timestamp_seconds",
			Help: "Unix timestamp of the last rule reconciliation",
		},
		[]string{"rule"},
	)
)

func init() {
	metrics.Registry.MustRegister(RulesTotal)
	metrics.Registry.MustRegister(TaintOperations)
	metrics.Registry.MustRegister(EvaluationDuration)
	metrics.Registry.MustRegister(Failures)
	metrics.Registry.MustRegister(BootstrapCompleted)
	metrics.Registry.MustRegister(ReconciliationLatency)
	metrics.Registry.MustRegister(BootstrapDuration)
	metrics.Registry.MustRegister(NodesByState)
	metrics.Registry.MustRegister(RuleLastReconciliationTime)
}

// CleanupRuleMetrics removes all Prometheus metrics associated with a deleted rule.
// This prevents memory leaks and "ghost" metrics in Grafana after a rule is deleted.
func CleanupRuleMetrics(ruleName string) {
	ruleLabel := prometheus.Labels{"rule": ruleName}

	// For metrics that only have the "rule" label, use Delete()
	BootstrapCompleted.Delete(ruleLabel)
	RuleLastReconciliationTime.Delete(ruleLabel)

	// For metrics with multiple labels (like "rule" + "state"), use DeletePartialMatch()
	TaintOperations.DeletePartialMatch(ruleLabel)
	EvaluationDuration.DeletePartialMatch(ruleLabel)
	Failures.DeletePartialMatch(ruleLabel)
	ReconciliationLatency.DeletePartialMatch(ruleLabel)
	BootstrapDuration.DeletePartialMatch(ruleLabel)
	NodesByState.DeletePartialMatch(ruleLabel)
}
