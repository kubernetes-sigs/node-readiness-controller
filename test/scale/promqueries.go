//go:build scale

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

package scale

// MetricQuery defines a query format for scraping Prometheus during scale tests.
type MetricQuery struct {
	Key       string
	QueryTmpl string
	Unit      string
	IsCounter bool
}

// metricQueries contains the definitions of all Prometheus queries used in the scale tests.
// Queries added here should also be added to the report template.
var metricQueries = []MetricQuery{
	{
		Key:       "reconcile_time_p50",
		QueryTmpl: "histogram_quantile(0.50, sum(rate(controller_runtime_reconcile_time_seconds_bucket{controller=\"node\",job=\"node-readiness-controller\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "reconcile_time_p90",
		QueryTmpl: "histogram_quantile(0.90, sum(rate(controller_runtime_reconcile_time_seconds_bucket{controller=\"node\",job=\"node-readiness-controller\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "reconcile_time_p99",
		QueryTmpl: "histogram_quantile(0.99, sum(rate(controller_runtime_reconcile_time_seconds_bucket{controller=\"node\",job=\"node-readiness-controller\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "reconciliation_latency_p50",
		QueryTmpl: "histogram_quantile(0.50, sum(rate(node_readiness_reconciliation_latency_seconds_bucket{rule=\"security-agent-readiness-rule\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "reconciliation_latency_p90",
		QueryTmpl: "histogram_quantile(0.90, sum(rate(node_readiness_reconciliation_latency_seconds_bucket{rule=\"security-agent-readiness-rule\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "reconciliation_latency_p99",
		QueryTmpl: "histogram_quantile(0.99, sum(rate(node_readiness_reconciliation_latency_seconds_bucket{rule=\"security-agent-readiness-rule\"}[%ds])) by (le))",
		Unit:      "s",
	},

	{
		Key:       "workqueue_queue_duration_p50",
		QueryTmpl: "histogram_quantile(0.50, sum(rate(workqueue_queue_duration_seconds_bucket{name=\"node\",job=\"node-readiness-controller\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "workqueue_queue_duration_p90",
		QueryTmpl: "histogram_quantile(0.90, sum(rate(workqueue_queue_duration_seconds_bucket{name=\"node\",job=\"node-readiness-controller\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "workqueue_queue_duration_p99",
		QueryTmpl: "histogram_quantile(0.99, sum(rate(workqueue_queue_duration_seconds_bucket{name=\"node\",job=\"node-readiness-controller\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "workqueue_work_duration_p50",
		QueryTmpl: "histogram_quantile(0.50, sum(rate(workqueue_work_duration_seconds_bucket{name=\"node\",job=\"node-readiness-controller\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "workqueue_work_duration_p90",
		QueryTmpl: "histogram_quantile(0.90, sum(rate(workqueue_work_duration_seconds_bucket{name=\"node\",job=\"node-readiness-controller\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "workqueue_work_duration_p99",
		QueryTmpl: "histogram_quantile(0.99, sum(rate(workqueue_work_duration_seconds_bucket{name=\"node\",job=\"node-readiness-controller\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "rule_evaluation_duration_p50",
		QueryTmpl: "histogram_quantile(0.50, sum(rate(node_readiness_evaluation_duration_seconds_bucket{job=\"node-readiness-controller\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "rule_evaluation_duration_p90",
		QueryTmpl: "histogram_quantile(0.90, sum(rate(node_readiness_evaluation_duration_seconds_bucket{job=\"node-readiness-controller\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "rule_evaluation_duration_p99",
		QueryTmpl: "histogram_quantile(0.99, sum(rate(node_readiness_evaluation_duration_seconds_bucket{job=\"node-readiness-controller\"}[%ds])) by (le))",
		Unit:      "s",
	},
	{
		Key:       "taint_operations_add",
		QueryTmpl: "sum(node_readiness_taint_operations_total{rule=\"security-agent-readiness-rule\",operation=\"add\"}) - (sum(node_readiness_taint_operations_total{rule=\"security-agent-readiness-rule\",operation=\"add\"} @ %.3f) or vector(0))",
		Unit:      "ops",
		IsCounter: true,
	},
	{
		Key:       "taint_operations_remove",
		QueryTmpl: "sum(node_readiness_taint_operations_total{rule=\"security-agent-readiness-rule\",operation=\"remove\"}) - (sum(node_readiness_taint_operations_total{rule=\"security-agent-readiness-rule\",operation=\"remove\"} @ %.3f) or vector(0))",
		Unit:      "ops",
		IsCounter: true,
	},

	{
		Key:       "condition_failures_total",
		QueryTmpl: "sum(node_readiness_condition_failures_total{rule=\"security-agent-readiness-rule\"}) - (sum(node_readiness_condition_failures_total{rule=\"security-agent-readiness-rule\"} @ %.3f) or vector(0))",
		Unit:      "failures",
		IsCounter: true,
	},
	{
		Key:       "operational_failures_total",
		QueryTmpl: "sum(node_readiness_failures_total{rule=\"security-agent-readiness-rule\"}) - (sum(node_readiness_failures_total{rule=\"security-agent-readiness-rule\"} @ %.3f) or vector(0))",
		Unit:      "failures",
		IsCounter: true,
	},
	{
		Key:       "api_conflicts_add_taint",
		QueryTmpl: "sum(node_readiness_api_conflicts_total{rule=\"security-agent-readiness-rule\",operation=\"add_taint\"}) - (sum(node_readiness_api_conflicts_total{rule=\"security-agent-readiness-rule\",operation=\"add_taint\"} @ %.3f) or vector(0))",
		Unit:      "conflicts",
		IsCounter: true,
	},
	{
		Key:       "api_conflicts_remove_taint",
		QueryTmpl: "sum(node_readiness_api_conflicts_total{rule=\"security-agent-readiness-rule\",operation=\"remove_taint\"}) - (sum(node_readiness_api_conflicts_total{rule=\"security-agent-readiness-rule\",operation=\"remove_taint\"} @ %.3f) or vector(0))",
		Unit:      "conflicts",
		IsCounter: true,
	},
	{
		Key:       "api_conflicts_rule_status_node_write",
		QueryTmpl: "sum(node_readiness_api_conflicts_total{rule=\"security-agent-readiness-rule\",operation=\"rule_status_node_write\"}) - (sum(node_readiness_api_conflicts_total{rule=\"security-agent-readiness-rule\",operation=\"rule_status_node_write\"} @ %.3f) or vector(0))",
		Unit:      "conflicts",
		IsCounter: true,
	},
	{
		Key:       "api_conflicts_rule_status_rule_sweep",
		QueryTmpl: "sum(node_readiness_api_conflicts_total{rule=\"security-agent-readiness-rule\",operation=\"rule_status_rule_sweep\"}) - (sum(node_readiness_api_conflicts_total{rule=\"security-agent-readiness-rule\",operation=\"rule_status_rule_sweep\"} @ %.3f) or vector(0))",
		Unit:      "conflicts",
		IsCounter: true,
	},
	{
		Key:       "api_conflicts_total",
		QueryTmpl: "sum(node_readiness_api_conflicts_total{rule=\"security-agent-readiness-rule\"}) - (sum(node_readiness_api_conflicts_total{rule=\"security-agent-readiness-rule\"} @ %.3f) or vector(0))",
		Unit:      "conflicts",
		IsCounter: true,
	},
	{
		Key:       "retry_exhaustion_add_taint",
		QueryTmpl: "sum(node_readiness_failures_total{rule=\"security-agent-readiness-rule\",reason=\"AddTaintConflictExhausted\"}) - (sum(node_readiness_failures_total{rule=\"security-agent-readiness-rule\",reason=\"AddTaintConflictExhausted\"} @ %.3f) or vector(0))",
		Unit:      "conflicts",
		IsCounter: true,
	},
	{
		Key:       "retry_exhaustion_remove_taint",
		QueryTmpl: "sum(node_readiness_failures_total{rule=\"security-agent-readiness-rule\",reason=\"RemoveTaintConflictExhausted\"}) - (sum(node_readiness_failures_total{rule=\"security-agent-readiness-rule\",reason=\"RemoveTaintConflictExhausted\"} @ %.3f) or vector(0))",
		Unit:      "conflicts",
		IsCounter: true,
	},
	{
		Key:       "retry_exhaustion_status_patch",
		QueryTmpl: "sum(node_readiness_failures_total{rule=\"security-agent-readiness-rule\",reason=\"StatusPatchConflictExhausted\"}) - (sum(node_readiness_failures_total{rule=\"security-agent-readiness-rule\",reason=\"StatusPatchConflictExhausted\"} @ %.3f) or vector(0))",
		Unit:      "conflicts",
		IsCounter: true,
	},
	{
		Key:       "retry_exhaustion_rule_status_sweep",
		QueryTmpl: "sum(node_readiness_failures_total{rule=\"security-agent-readiness-rule\",reason=\"RuleStatusRuleSweepConflictExhausted\"}) - (sum(node_readiness_failures_total{rule=\"security-agent-readiness-rule\",reason=\"RuleStatusRuleSweepConflictExhausted\"} @ %.3f) or vector(0))",
		Unit:      "conflicts",
		IsCounter: true,
	},
	{
		Key:       "rule_controller_reconcile_errors",
		QueryTmpl: "sum(controller_runtime_reconcile_errors_total{controller=\"nodereadiness-controller\",job=\"node-readiness-controller\"}) - (sum(controller_runtime_reconcile_errors_total{controller=\"nodereadiness-controller\",job=\"node-readiness-controller\"} @ %.3f) or vector(0))",
		Unit:      "errors",
		IsCounter: true,
	},

	{
		Key:       "cpu_cores_rate",
		QueryTmpl: "sum(rate(process_cpu_seconds_total{job=\"node-readiness-controller\"}[%ds]))",
		Unit:      "cores",
	},
	{
		Key:       "cpu_cores_peak",
		QueryTmpl: "max_over_time(sum(rate(process_cpu_seconds_total{job=\"node-readiness-controller\"}[5s]))[%ds:1s])",
		Unit:      "cores",
	},
	{
		Key:       "resident_memory_peak",
		QueryTmpl: "max(max_over_time(process_resident_memory_bytes{job=\"node-readiness-controller\"}[%ds]))",
		Unit:      "bytes",
	},
	{
		Key:       "resident_memory_avg",
		QueryTmpl: "avg(avg_over_time(process_resident_memory_bytes{job=\"node-readiness-controller\"}[%ds]))",
		Unit:      "bytes",
	},
	{
		Key:       "workqueue_retries_node",
		QueryTmpl: "sum(workqueue_retries_total{name=\"node\",job=\"node-readiness-controller\"}) - (sum(workqueue_retries_total{name=\"node\",job=\"node-readiness-controller\"} @ %.3f) or vector(0))",
		Unit:      "retries",
		IsCounter: true,
	},
	{
		Key:       "workqueue_retries_rules",
		QueryTmpl: "sum(workqueue_retries_total{name=\"nodereadiness-controller\",job=\"node-readiness-controller\"}) - (sum(workqueue_retries_total{name=\"nodereadiness-controller\",job=\"node-readiness-controller\"} @ %.3f) or vector(0))",
		Unit:      "retries",
		IsCounter: true,
	},
	{
		Key:       "workqueue_adds_node",
		QueryTmpl: "sum(workqueue_adds_total{name=\"node\",job=\"node-readiness-controller\"}) - (sum(workqueue_adds_total{name=\"node\",job=\"node-readiness-controller\"} @ %.3f) or vector(0))",
		Unit:      "adds",
		IsCounter: true,
	},
	{
		Key:       "workqueue_adds_rules",
		QueryTmpl: "sum(workqueue_adds_total{name=\"nodereadiness-controller\",job=\"node-readiness-controller\"}) - (sum(workqueue_adds_total{name=\"nodereadiness-controller\",job=\"node-readiness-controller\"} @ %.3f) or vector(0))",
		Unit:      "adds",
		IsCounter: true,
	},
	{
		Key:       "kube_api_requests_total",
		QueryTmpl: "sum(rest_client_requests_total{job=\"node-readiness-controller\"}) - (sum(rest_client_requests_total{job=\"node-readiness-controller\"} @ %.3f) or vector(0))",
		Unit:      "requests",
		IsCounter: true,
	},
	{
		Key:       "kube_api_requests_rate",
		QueryTmpl: "sum(rate(rest_client_requests_total{job=\"node-readiness-controller\"}[%ds]))",
		Unit:      "req/s",
	},
}
