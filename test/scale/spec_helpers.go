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

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
	. "github.com/onsi/gomega"    //nolint:staticcheck
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/node-readiness-controller/test/utils"
)

type prometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Value []interface{} `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

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
	Phases    []PhaseJSON `json:"phases"`
}

type PhaseJSON struct {
	Phase           string         `json:"phase"`
	DurationSeconds float64        `json:"duration_seconds"`
	Latencies       LatenciesJSON  `json:"latencies"`
	Resources       ResourcesJSON  `json:"resources"`
	Workqueue       WorkqueueJSON  `json:"workqueue"`
	Operations      OperationsJSON `json:"operations"`
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

func buildPhaseJSON(q queryResult) PhaseJSON {
	getRaw := func(key string) float64 {
		return q.RawMetrics[key]
	}
	getInt := func(key string) int64 {
		return int64(q.RawMetrics[key])
	}

	return PhaseJSON{
		Phase:           q.Phase,
		DurationSeconds: q.DurationSeconds,
		Latencies: LatenciesJSON{
			ReconcileTime: PercentilesJSON{
				P50: getRaw("reconcile_time_p50"),
				P90: getRaw("reconcile_time_p90"),
				P99: getRaw("reconcile_time_p99"),
			},
			ReconciliationLatency: PercentilesJSON{
				P50: getRaw("reconciliation_latency_p50"),
				P90: getRaw("reconciliation_latency_p90"),
				P99: getRaw("reconciliation_latency_p99"),
			},
			RuleEvaluationDuration: PercentilesJSON{
				P50: getRaw("rule_evaluation_duration_p50"),
				P90: getRaw("rule_evaluation_duration_p90"),
				P99: getRaw("rule_evaluation_duration_p99"),
			},
			WorkqueueQueueDuration: PercentilesJSON{
				P50: getRaw("workqueue_queue_duration_p50"),
				P90: getRaw("workqueue_queue_duration_p90"),
				P99: getRaw("workqueue_queue_duration_p99"),
			},
			WorkqueueWorkDuration: PercentilesJSON{
				P50: getRaw("workqueue_work_duration_p50"),
				P90: getRaw("workqueue_work_duration_p90"),
				P99: getRaw("workqueue_work_duration_p99"),
			},
		},
		Resources: ResourcesJSON{
			CPUCores: CPUUsageJSON{
				Rate: getRaw("cpu_cores_rate"),
				Peak: getRaw("cpu_cores_peak"),
			},
			ResidentMemory: MemoryUsageJSON{
				AvgBytes:  getRaw("resident_memory_avg"),
				PeakBytes: getRaw("resident_memory_peak"),
			},
		},
		Workqueue: WorkqueueJSON{
			Node: ControllerWorkqueueJSON{
				Adds:    getInt("workqueue_adds_node"),
				Retries: getInt("workqueue_retries_node"),
			},
			Rules: ControllerWorkqueueJSON{
				Adds:    getInt("workqueue_adds_rules"),
				Retries: getInt("workqueue_retries_rules"),
			},
		},
		Operations: OperationsJSON{
			TaintsAdded:         getInt("taint_operations_add"),
			TaintsRemoved:       getInt("taint_operations_remove"),
			ConditionFailures:   getInt("condition_failures_total"),
			OperationalFailures: getInt("operational_failures_total"),
		},
	}
}

var (
	// We are using client-go over kubectl to increase the polling frequency when counting nodes.
	clientset *kubernetes.Clientset
	// We need an HTTP client to query Prometheus endpoint.
	promHTTPClient = &http.Client{Timeout: 5 * time.Second}
)

func getKubeClient() (*kubernetes.Clientset, error) {
	if clientset != nil {
		return clientset, nil
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}

	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	clientset = cs
	return clientset, nil
}

func applyManifest(ctx context.Context, manifest string) {
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	output, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to apply manifest:\n%s", output)
}

func deleteStage(ctx context.Context, stageName string) {
	cmd := exec.CommandContext(ctx, "kubectl", "delete", "stage", stageName, "--ignore-not-found")
	output, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to delete stage %s:\n%s", stageName, output)
}

func countKwokNodes(ctx context.Context, labelSelector string) (int, error) {
	client, err := getKubeClient()
	if err != nil {
		return 0, err
	}

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return 0, err
	}

	return len(nodes.Items), nil
}

func countTaintedNodes(ctx context.Context, labelSelector string, taintKey string, taintValue string) (int, error) {
	client, err := getKubeClient()
	if err != nil {
		return 0, err
	}

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return 0, err
	}

	count := 0
	for _, node := range nodes.Items {
		for _, taint := range node.Spec.Taints {
			if taint.Key == taintKey && taint.Value == taintValue {
				count++
				break
			}
		}
	}
	return count, nil
}

func waitForNodeTaints(ctx context.Context, targetTaintedCount int, timeout string) {
	Eventually(func(g Gomega) int {
		count, err := countTaintedNodes(ctx, "type=kwok", "readiness.k8s.io/SecurityAgentNotReady", "pending")
		g.Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("Progress: %d/%d nodes tainted", count, cfg.NodeCount))
		return count
	}, timeout, "1s").Should(Equal(targetTaintedCount), "Tainted node count did not reach expected target")
}

func queryPrometheusInstant(ctx context.Context, query string, ts float64) (string, error) {
	// Construct the Prometheus Instant Query HTTP endpoint.
	// Query parameters are URL-escaped, and the evaluation timestamp float is formatted to 3 decimal places.
	urlStr := fmt.Sprintf("http://127.0.0.1:%s/api/v1/query?query=%s&time=%.3f", cfg.PrometheusPort, url.QueryEscape(query), ts)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", err
	}

	resp, err := promHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var promResp prometheusResponse

	if err := json.NewDecoder(resp.Body).Decode(&promResp); err != nil {
		return "", err
	}

	if promResp.Status != "success" {
		return "", fmt.Errorf("prometheus query failed: %s", promResp.Status)
	}

	// Prometheus instant query response format:
	// "result": [{"metric": {}, "value": [ <timestamp_float>, "<value_string>" ]}]
	// We verify that we received at least one time-series result, and that the value array
	// has at least two elements (timestamp at index 0, metric value string at index 1).
	if len(promResp.Data.Result) == 0 || len(promResp.Data.Result[0].Value) < 2 {
		return "", fmt.Errorf("no data returned")
	}

	valStr, ok := promResp.Data.Result[0].Value[1].(string)
	if !ok {
		return "", fmt.Errorf("invalid value format")
	}
	return valStr, nil
}

func collectMetricsForPhase(ctx context.Context, phaseStart time.Time, phaseEnd time.Time) map[string]string {
	// Add a 5-second offset to the query time. Prometheus scrapes metrics asynchronously,
	// so querying exactly at phaseEnd might miss metrics events that occurred in the last second
	// of the phase because they haven't been scraped and written to the database yet.
	queryTime := phaseEnd.Add(5 * time.Second)

	// Calculate the range duration (in seconds) from the start of the phase up to our
	// offset query time. This is used as the range vector window (e.g. [45s]) for gauges and rates.
	lookbackSecs := int(queryTime.Sub(phaseStart).Seconds())

	// Convert the offset query timestamp into a float64 Unix epoch (seconds with sub-second precision).
	// The Prometheus API expects the query evaluation time parameter to be formatted as a float.
	ts := float64(queryTime.UnixNano()) / 1e9

	metricsMap := make(map[string]string)

	for _, q := range metricQueries {
		var val string
		var err error

		if q.IsCounter {
			// For counters, we calculate the exact delta increase over the phase.
			// We format the phase start time as a float Unix timestamp and inject it
			// into the PromQL query template using the '@' modifier.
			tsStart := float64(phaseStart.UnixNano()) / 1e9
			queryStr := fmt.Sprintf(q.QueryTmpl, tsStart)

			// Execute the instant query at the end-of-phase timestamp (ts).
			// This returns: Value(end) - (Value(start) or 0).
			val, err = queryPrometheusInstant(ctx, queryStr, ts)
			if err != nil {
				metricsMap[q.Key] = "0"
				continue
			}
		} else {
			// For non-counter metrics (gauges and histograms), we evaluate them over the
			// sliding range window defined by lookbackSecs (e.g., avg_over_time(metric[45s])).
			queryStr := fmt.Sprintf(q.QueryTmpl, lookbackSecs)

			// Query the statistic evaluated at the end-of-phase timestamp (ts).
			val, err = queryPrometheusInstant(ctx, queryStr, ts)
			if err != nil {
				metricsMap[q.Key] = "N/A"
				continue
			}
		}

		metricsMap[q.Key] = val
	}

	return metricsMap
}

func formatMetricValue(val string, unit string) string {
	if val == "N/A" || val == "" {
		return val
	}
	if unit == "s" || unit == "cores" {
		if floatVal, err := strconv.ParseFloat(val, 64); err == nil {
			return fmt.Sprintf("%.3f %s", floatVal, unit)
		}
	}
	if unit == "bytes" {
		if floatVal, err := strconv.ParseFloat(val, 64); err == nil {
			mb := floatVal / (1024 * 1024)
			return fmt.Sprintf("%.2f MB", mb)
		}
	}
	if unit != "" {
		return fmt.Sprintf("%s %s", val, unit)
	}
	return val
}

func buildReportForPhase(phaseName string, phaseTitle string, phaseStart time.Time, phaseEnd time.Time, metricsMap map[string]string) queryResult {
	formattedMetrics := make(map[string]string, len(metricsMap))
	rawMetrics := make(map[string]float64, len(metricsMap))

	for _, q := range metricQueries {
		metricValue, ok := metricsMap[q.Key]
		if !ok {
			continue
		}

		formattedMetrics[q.Key] = formatMetricValue(metricValue, q.Unit)

		if floatVal, err := strconv.ParseFloat(metricValue, 64); err == nil && !math.IsNaN(floatVal) && !math.IsInf(floatVal, 0) {
			rawMetrics[q.Key] = floatVal
		}
	}

	return queryResult{
		Phase:           phaseName,
		PhaseTitle:      phaseTitle,
		DurationSeconds: phaseEnd.Sub(phaseStart).Seconds(),
		Metrics:         formattedMetrics,
		RawMetrics:      rawMetrics,
	}
}

func collectAndRecordPhaseMetrics(ctx context.Context, phases []phaseStats) {
	for _, phase := range phases {
		metricsMap := collectMetricsForPhase(ctx, phase.start, phase.end)
		reportStruct := buildReportForPhase(phase.phase, phase.title, phase.start, phase.end, metricsMap)
		queryResults = append(queryResults, reportStruct)
	}
}
