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
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
	. "github.com/onsi/gomega"    //nolint:staticcheck
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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
	PhaseTitle      string            `json:"phase_title"`
	DurationSeconds float64           `json:"duration_seconds"`
	Metrics         map[string]string `json:"metrics"`
}

var (
	// We are using client-go over kubectl to increase the polling frequency when counting nodes.
	clientset *kubernetes.Clientset
	// We need an HTTP client to query Prometheus endpoint.
	promHTTPClient = &http.Client{Timeout: 5 * time.Second}
)

func ensureKwokctl(version string, targetDir string) string {
	goOS := runtime.GOOS
	goArch := runtime.GOARCH

	binaryName := "kwokctl"
	if goOS == "windows" {
		binaryName += ".exe"
	}
	localBinaryPath := filepath.Join(targetDir, binaryName)

	if _, err := os.Stat(localBinaryPath); err == nil {
		return localBinaryPath
	}

	err := os.MkdirAll(targetDir, 0750)
	Expect(err).NotTo(HaveOccurred(), "Failed to create tools directory structure")
	downloadURL := fmt.Sprintf(
		"https://github.com/kubernetes-sigs/kwok/releases/download/%s/kwokctl-%s-%s",
		version, goOS, goArch,
	)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, downloadURL, nil)
	Expect(err).NotTo(HaveOccurred(), "Failed to create download request")
	resp, err := http.DefaultClient.Do(req) // #nosec G107
	Expect(err).NotTo(HaveOccurred(), "Failed to initiate kwokctl binary download")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		Fail(fmt.Sprintf("Failed to download kwokctl from URL %s: Status %s", downloadURL, resp.Status))
	}

	out, err := os.OpenFile(localBinaryPath, os.O_CREATE|os.O_WRONLY, 0700) // #nosec G304 G302
	Expect(err).NotTo(HaveOccurred(), "Failed to create local binary destination file")
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, resp.Body)
	Expect(err).NotTo(HaveOccurred(), "Failed to write binary content to disk target")

	return localBinaryPath
}

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

func countKwokNodes(ctx context.Context) (int, error) {
	client, err := getKubeClient()
	if err != nil {
		return 0, err
	}

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "type=kwok",
	})
	if err != nil {
		return 0, err
	}

	return len(nodes.Items), nil
}

func countTaintedNodes(ctx context.Context) (int, error) {
	client, err := getKubeClient()
	if err != nil {
		return 0, err
	}

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "type=kwok",
	})
	if err != nil {
		return 0, err
	}

	count := 0
	for _, node := range nodes.Items {
		for _, taint := range node.Spec.Taints {
			if taint.Key == "readiness.k8s.io/SecurityAgentNotReady" && taint.Value == "pending" {
				count++
				break
			}
		}
	}
	return count, nil
}

func queryPrometheusInstant(ctx context.Context, query string, ts float64) (string, error) {
	// Construct the Prometheus Instant Query HTTP endpoint.
	// Query parameters are URL-escaped, and the evaluation timestamp float is formatted to 3 decimal places.
	urlStr := fmt.Sprintf("http://127.0.0.1:%s/api/v1/query?query=%s&time=%.3f", prometheusPort, url.QueryEscape(query), ts)

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

//nolint:unparam
func collectAndReportMetricsForWindow(ctx context.Context, phaseTitle string, phaseStart time.Time, phaseEnd time.Time) (queryResult, error) {
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
				metricsMap[q.Key] = "0 " + q.Unit
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

		// Append the display unit (e.g., "s", "ops", "cores") to the formatted string.
		if q.Unit != "" {
			metricsMap[q.Key] = val + " " + q.Unit
		} else {
			metricsMap[q.Key] = val
		}
	}

	res := queryResult{
		PhaseTitle:      phaseTitle,
		DurationSeconds: phaseEnd.Sub(phaseStart).Seconds(),
		Metrics:         metricsMap,
	}

	return res, nil
}
