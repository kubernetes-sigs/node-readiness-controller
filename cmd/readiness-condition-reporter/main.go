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
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

const (
	envNodeName            = "NODE_NAME"
	envConditionType       = "CONDITION_TYPE"
	envCheckEndpoint       = "CHECK_ENDPOINT"
	envCheckInterval       = "CHECK_INTERVAL"
	envImpersonateNode     = "IMPERSONATE_NODE"
	envHeartbeatPeriod     = "HEARTBEAT_PERIOD"
	envMetricsBindAddress  = "METRICS_BIND_ADDRESS"
	defaultCheckInterval   = 30 * time.Second
	defaultHTTPTimeout     = 10 * time.Second
	defaultHeartbeatPeriod = 5 * time.Minute
	defaultMetricsBindAddr = ":9445"
	metricsShutdownTimeout = 5 * time.Second
)

// Reason values set on HealthResponse.Reason by checkHealth, and classified
// by runCheck into the reporterChecksTotal result label.
const (
	ReasonEndpointOK              = "EndpointOK"
	ReasonEndpointNotReady        = "EndpointNotReady"
	ReasonEndpointConnectionError = "EndpointConnectionError"
	ReasonRequestCreationError    = "RequestCreationError"
	ReasonHealthCheckFailed       = "HealthCheckFailed"
)

// HealthResponse represents the health check response structure.
type HealthResponse struct {
	Healthy bool   `json:"healthy"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	// Get configuration from environment
	nodeName := os.Getenv(envNodeName)
	if nodeName == "" {
		klog.ErrorS(nil, "Environment variable not set", "variable", envNodeName)
		klog.Flush()
		os.Exit(1)
	}

	conditionType := os.Getenv(envConditionType)
	if conditionType == "" {
		klog.ErrorS(nil, "Environment variable not set", "variable", envConditionType)
		klog.Flush()
		os.Exit(1)
	}

	checkEndpoint, err := validateCheckEndpoint(os.Getenv(envCheckEndpoint))

	if err != nil {
		klog.ErrorS(err, "Invalid check endpoint configuration", "variable", envCheckEndpoint)
		klog.Flush()
		os.Exit(1)
	}

	checkInterval := os.Getenv(envCheckInterval)
	interval := defaultCheckInterval
	if checkInterval != "" {
		parsedInterval, err := time.ParseDuration(checkInterval)
		if err == nil {
			interval = parsedInterval
		} else {
			klog.ErrorS(err, "Failed to parse check interval, using default",
				"input", checkInterval,
				"default", defaultCheckInterval)
		}
	}

	heartbeatPeriodStr := os.Getenv(envHeartbeatPeriod)
	heartbeatPeriod := defaultHeartbeatPeriod
	if heartbeatPeriodStr != "" {
		parsedPeriod, err := time.ParseDuration(heartbeatPeriodStr)
		if err == nil {
			heartbeatPeriod = parsedPeriod
		} else {
			klog.ErrorS(err, "Failed parse heartbeat period, using default",
				"input", heartbeatPeriodStr,
				"default", defaultHeartbeatPeriod)
		}
	}

	metricsBindAddrStr := os.Getenv(envMetricsBindAddress)
	metricsBindAddr := defaultMetricsBindAddr
	if metricsBindAddrStr != "" {
		metricsBindAddr = metricsBindAddrStr
	}

	// Create Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.ErrorS(err, "Failed to create in-cluster config")
		klog.Flush()
		os.Exit(1)
	}

	// Set the constrained impersonation config
	if os.Getenv(envImpersonateNode) == "true" {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: "system:node:" + nodeName,
		}
		klog.InfoS("Node impersonation enabled", "impersonating", config.Impersonate.UserName)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.ErrorS(err, "Failed to create client")
		klog.Flush()
		os.Exit(1)
	}

	httpClient := &http.Client{
		Timeout: defaultHTTPTimeout,
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	metricsServer := &http.Server{
		Addr:              metricsBindAddr,
		Handler:           mux,
		ReadHeaderTimeout: defaultHTTPTimeout,
	}
	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.ErrorS(err, "Metrics server failed", "address", metricsBindAddr)
		}
	}()

	// Create a context that cancels on SIGTERM or SIGINT
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	klog.InfoS("Starting readiness condition reporter", "node", nodeName, "condition", conditionType, "interval", interval, "metricsBindAddress", metricsBindAddr)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on startup, then on each tick
	runCheck(ctx, httpClient, clientset, checkEndpoint, nodeName, conditionType, heartbeatPeriod)
	for {
		select {
		case <-ctx.Done():
			klog.InfoS("Shutting down readiness condition reporter", "reason", ctx.Err())
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), metricsShutdownTimeout)
			defer shutdownCancel()

			if err := metricsServer.Shutdown(shutdownCtx); err != nil {
				klog.ErrorS(err, "Failed to gracefully shut down metrics server")
			}
			return
		case <-ticker.C:
			runCheck(ctx, httpClient, clientset, checkEndpoint, nodeName, conditionType, heartbeatPeriod)
		}
	}
}

// runCheck performs a single health check and updates the node condition.
func runCheck(ctx context.Context, httpClient *http.Client, clientset kubernetes.Interface, checkEndpoint, nodeName, conditionType string, heartbeatPeriod time.Duration) {
	start := time.Now()
	health, err := checkHealth(ctx, httpClient, checkEndpoint)
	reporterCheckDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		klog.ErrorS(err, "Health check failed", "endpoint", checkEndpoint)
		health = &HealthResponse{
			Healthy: false,
			Reason:  ReasonHealthCheckFailed,
			Message: fmt.Sprintf("Health check failed: %v", err),
		}
	}

	switch health.Reason {
	case ReasonEndpointOK:
		reporterChecksTotal.WithLabelValues("healthy").Inc()
	case ReasonEndpointNotReady:
		reporterChecksTotal.WithLabelValues("unhealthy").Inc()
	default:
		reporterChecksTotal.WithLabelValues("error").Inc()
	}

	if err := updateNodeCondition(ctx, clientset, nodeName, conditionType, health, heartbeatPeriod); err != nil {
		klog.ErrorS(err, "Failed to update node condition", "node", nodeName, "condition", conditionType)
		reporterConditionWritesTotal.WithLabelValues("error").Inc()
	}
}

// validateCheckEndpoint ensures the health check endpoint is a well-formed HTTP(S) URL.
func validateCheckEndpoint(endpoint string) (string, error) {
	if endpoint == "" {
		return "", fmt.Errorf("endpoint cannot be empty")
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q: must be http or https", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("missing host in endpoint URL")
	}

	return endpoint, nil
}

// checkHealth performs an HTTP request to check component health.
func checkHealth(ctx context.Context, client *http.Client, endpoint string) (*HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil) //nolint:gosec // endpoint validated at startup
	if err != nil {
		return &HealthResponse{
			Healthy: false,
			Reason:  ReasonRequestCreationError,
			Message: fmt.Sprintf("Failed to create request for endpoint %s: %v", endpoint, err),
		}, nil
	}

	resp, err := client.Do(req) //nolint:gosec // endpoint validated at startup
	if err != nil {
		return &HealthResponse{
			Healthy: false,
			Reason:  ReasonEndpointConnectionError,
			Message: fmt.Sprintf("Failed to reach endpoint %s: %v", endpoint, err),
		}, nil
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return &HealthResponse{
			Healthy: true,
			Reason:  ReasonEndpointOK,
			Message: fmt.Sprintf("Endpoint reports ready at %s", endpoint),
		}, nil
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	bodyString := ""
	if err == nil {
		bodyString = string(bodyBytes)
	} else {
		klog.ErrorS(err, "Failed to read response body", "endpoint", endpoint)
		bodyString = "<failed to read response body>"
	}

	return &HealthResponse{
		Healthy: false,
		Reason:  ReasonEndpointNotReady,
		Message: fmt.Sprintf("Endpoint returned non-2xx status code %d at %s: %s", resp.StatusCode, endpoint, bodyString),
	}, nil
}

// updateNodeCondition updates the node condition based on health check.
func updateNodeCondition(ctx context.Context, client kubernetes.Interface, nodeName, conditionType string, health *HealthResponse, heartbeatPeriod time.Duration) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get the node
		node, err := client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Create new condition
		now := metav1.NewTime(time.Now())
		status := corev1.ConditionFalse
		if health.Healthy {
			status = corev1.ConditionTrue
		}

		// Find existing condition to preserve transition time if status hasn't changed
		var transitionTime metav1.Time
		var existingCondition *corev1.NodeCondition

		for _, condition := range node.Status.Conditions {
			if string(condition.Type) == conditionType {
				condCopy := condition
				existingCondition = &condCopy
				if condition.Status == status {
					transitionTime = condition.LastTransitionTime
				}
				break
			}
		}

		// If the semantic state is completely unchanged, bypass the API write
		// to prevent etcd write amplification and control plane flooding.
		needsUpdate := true
		if existingCondition != nil && existingCondition.Status == status && existingCondition.Reason == health.Reason && existingCondition.Message == health.Message {
			needsUpdate = false
			/*
				NOTE: Skipping the write stops refreshing the LastHeartbeatTime on every tick.
				To mitigate this, force an update every 5 minutes even if the state is unchanged.
			*/
			if time.Since(existingCondition.LastHeartbeatTime.Time) >= heartbeatPeriod {
				needsUpdate = true
			}
		}

		if !needsUpdate {
			// state has not changed for specified period, skip the write
			klog.V(4).InfoS("Condition state unchanged, skipping node status update", "node", nodeName, "condition", conditionType)
			reporterConditionWritesTotal.WithLabelValues("skipped").Inc()
			return nil
		}

		if transitionTime.IsZero() {
			transitionTime = now
		}

		// Create condition
		condition := corev1.NodeCondition{
			Type:               corev1.NodeConditionType(conditionType),
			Status:             status,
			LastHeartbeatTime:  now,
			LastTransitionTime: transitionTime,
			Reason:             health.Reason,
			Message:            health.Message,
		}

		// Update node status
		found := false
		for i, c := range node.Status.Conditions {
			if string(c.Type) == conditionType {
				node.Status.Conditions[i] = condition
				found = true
				break
			}
		}

		if !found {
			node.Status.Conditions = append(node.Status.Conditions, condition)
		}

		_, err = client.CoreV1().Nodes().UpdateStatus(ctx, node, metav1.UpdateOptions{})
		if err == nil {
			reporterConditionWritesTotal.WithLabelValues("success").Inc()
		}
		return err
	})
}
