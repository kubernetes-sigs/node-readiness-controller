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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	envRunMode             = "RUN_MODE"
	envImpersonateNode     = "IMPERSONATE_NODE"
	envHeartbeatPeriod     = "HEARTBEAT_PERIOD"
	defaultCheckInterval   = 30 * time.Second
	defaultHTTPTimeout     = 10 * time.Second
	defaultHeartbeatPeriod = 5 * time.Minute

	// Supported run modes for the reporter. These intentionally mirror the
	// enforcementMode values of the NodeReadinessRule CRD so the reporter can
	// be deployed as the per-node counterpart of a bootstrap-only/continuous rule.
	runModeContinuous    = "continuous"
	runModeBootstrapOnly = "bootstrap-only"
	defaultRunMode       = runModeContinuous
)

// HealthResponse represents the health check response structure.
type HealthResponse struct {
	Healthy bool   `json:"healthy"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func main() {
	klog.InitFlags(nil)

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

	runMode := os.Getenv(envRunMode)
	if runMode == "" {
		runMode = defaultRunMode
	}
	if err := validateRunMode(runMode); err != nil {
		klog.ErrorS(err, "Invalid run mode configuration", "variable", envRunMode)
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

	// Create a context that cancels on SIGTERM or SIGINT
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	klog.InfoS("Starting readiness condition reporter", "node", nodeName, "condition", conditionType, "interval", interval, "runMode", runMode)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on startup, then on each tick. In bootstrap-only mode the
	// reporter exits as soon as the component becomes healthy; in continuous mode
	// it polls until SIGTERM/SIGINT.
	for {
		health := runCheck(ctx, httpClient, clientset, checkEndpoint, nodeName, conditionType, heartbeatPeriod)
		if shouldExitBootstrap(runMode, health) {
			klog.InfoS("Bootstrap-only mode: component became healthy, exiting", "node", nodeName, "condition", conditionType)
			return
		}

		select {
		case <-ctx.Done():
			klog.InfoS("Shutting down readiness condition reporter", "reason", ctx.Err())
			return
		case <-ticker.C:
		}
	}
}

// shouldExitBootstrap reports whether the reporter should exit after a health
// check. It returns true only in bootstrap-only mode once the component has
// become healthy (and its node condition updated for the final time). In
// continuous mode it always returns false so the reporter keeps polling.
func shouldExitBootstrap(runMode string, health *HealthResponse) bool {
	return runMode == runModeBootstrapOnly && health != nil && health.Healthy
}

// runCheck performs a single health check and updates the node condition.
func runCheck(ctx context.Context, httpClient *http.Client, clientset kubernetes.Interface, checkEndpoint, nodeName, conditionType string, heartbeatPeriod time.Duration) *HealthResponse {
	health, err := checkHealth(ctx, httpClient, checkEndpoint)
	if err != nil {
		klog.ErrorS(err, "Health check failed", "endpoint", checkEndpoint)
		health = &HealthResponse{
			Healthy: false,
			Reason:  "HealthCheckFailed",
			Message: fmt.Sprintf("Health check failed: %v", err),
		}
	}

	if err := updateNodeCondition(ctx, clientset, nodeName, conditionType, health, heartbeatPeriod); err != nil {
		klog.ErrorS(err, "Failed to update node condition", "node", nodeName, "condition", conditionType)
	}

	return health
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

// validateRunMode validates the RUN_MODE environment variable.
func validateRunMode(mode string) error {
	switch mode {
	case runModeContinuous, runModeBootstrapOnly:
		return nil
	default:
		return fmt.Errorf("unsupported run mode %q: must be %q or %q", mode, runModeContinuous, runModeBootstrapOnly)
	}
}

// checkHealth performs an HTTP request to check component health.
func checkHealth(ctx context.Context, client *http.Client, endpoint string) (*HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil) //nolint:gosec // endpoint validated at startup
	if err != nil {
		return &HealthResponse{
			Healthy: false,
			Reason:  "RequestCreationError",
			Message: fmt.Sprintf("Failed to create request for endpoint %s: %v", endpoint, err),
		}, nil
	}

	resp, err := client.Do(req) //nolint:gosec // endpoint validated at startup
	if err != nil {
		return &HealthResponse{
			Healthy: false,
			Reason:  "EndpointConnectionError",
			Message: fmt.Sprintf("Failed to reach endpoint %s: %v", endpoint, err),
		}, nil
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return &HealthResponse{
			Healthy: true,
			Reason:  "EndpointOK",
			Message: fmt.Sprintf("Endpoint reports ready at %s", endpoint),
		}, nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	bodyString := ""
	if err == nil {
		bodyString = string(bodyBytes)
	} else {
		klog.ErrorS(err, "Failed to read response body", "endpoint", endpoint)
		bodyString = "<failed to read response body>"
	}

	return &HealthResponse{
		Healthy: false,
		Reason:  "EndpointNotReady",
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
		return err
	})
}
