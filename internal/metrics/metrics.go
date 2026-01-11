package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// ActiveRules tracks the number of currently active NodeReadinessRules.
	ActiveRules = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "node_readiness_active_rules",
			Help: "Number of active NodeReadinessRules",
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
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(ActiveRules)
	metrics.Registry.MustRegister(TaintOperations)
	metrics.Registry.MustRegister(EvaluationDuration)
	metrics.Registry.MustRegister(Failures)
	metrics.Registry.MustRegister(BootstrapCompleted)
}
