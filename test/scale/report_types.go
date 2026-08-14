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

type queryResult struct {
	Phase           string             `json:"phase"`
	PhaseTitle      string             `json:"phase_title"`
	DurationSeconds float64            `json:"duration_seconds"`
	Metrics         map[string]string  `json:"metrics"`
	RawMetrics      map[string]float64 `json:"raw_metrics"`
}

type ScalabilityReportJSON struct {
	NodeCount int         `json:"node_count"`
	Mode      string      `json:"mode"`
	Runtime   string      `json:"runtime"`
	Phases    []PhaseJSON `json:"phases"`
}

type PhaseJSON struct {
	Phase           string         `json:"phase"`
	DurationSeconds float64        `json:"duration_seconds"`
	Latencies       LatenciesJSON  `json:"latencies"`
	Resources       ResourcesJSON  `json:"resources"`
	Workqueue       WorkqueueJSON  `json:"workqueue"`
	Operations      OperationsJSON `json:"operations"`
	APIClient       APIClientJSON  `json:"api_client"`
}

type PercentilesJSON struct {
	P50 float64 `json:"p50"`
	P90 float64 `json:"p90"`
	P99 float64 `json:"p99"`
}

type LatenciesJSON struct {
	ReconcileTime          PercentilesJSON `json:"reconcile_time"`
	ReconciliationLatency  PercentilesJSON `json:"reconciliation_latency"`
	RuleEvaluationDuration PercentilesJSON `json:"rule_evaluation_duration"`
	WorkqueueQueueDuration PercentilesJSON `json:"workqueue_queue_duration"`
	WorkqueueWorkDuration  PercentilesJSON `json:"workqueue_work_duration"`
}

type CPUUsageJSON struct {
	Rate float64 `json:"rate"`
	Peak float64 `json:"peak"`
}

type MemoryUsageJSON struct {
	AvgBytes  float64 `json:"avg_bytes"`
	PeakBytes float64 `json:"peak_bytes"`
}

type ResourcesJSON struct {
	CPUCores       CPUUsageJSON    `json:"cpu_cores"`
	ResidentMemory MemoryUsageJSON `json:"resident_memory"`
}

type ControllerWorkqueueJSON struct {
	Adds    int64 `json:"adds"`
	Retries int64 `json:"retries"`
}

type WorkqueueJSON struct {
	Node  ControllerWorkqueueJSON `json:"node"`
	Rules ControllerWorkqueueJSON `json:"rules"`
}

type OperationsJSON struct {
	TaintsAdded         int64 `json:"taints_added"`
	TaintsRemoved       int64 `json:"taints_removed"`
	ConditionFailures   int64 `json:"condition_failures"`
	OperationalFailures int64 `json:"operational_failures"`
}

type APIClientJSON struct {
	RequestsTotal int64   `json:"requests_total"`
	RequestsRate  float64 `json:"requests_rate"`
}
