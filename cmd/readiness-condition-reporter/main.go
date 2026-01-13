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
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

const (
	envNodeName          = "NODE_NAME"
	envConditionType     = "CONDITION_TYPE"
	envCheckEndpoint     = "CHECK_ENDPOINT"
	envCheckInterval     = "CHECK_INTERVAL"
	defaultCheckInterval = 30 * time.Second
	defaultHTTPTimeout   = 10 * time.Second
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
		os.Exit(1)
	}

	conditionType := os.Getenv(envConditionType)
	if conditionType == "" {
		klog.ErrorS(nil, "Environment variable not set", "variable", envConditionType)
		os.Exit(1)
	}

	checkEndpoint := os.Getenv(envCheckEndpoint)
	if checkEndpoint == "" {
		klog.ErrorS(nil, "Environment variable not set", "variable", envCheckEndpoint)
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

	// Create Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.ErrorS(err, "Failed to create in-cluster config")
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.ErrorS(err, "Failed to create client")
		os.Exit(1)
	}

	httpClient := &http.Client{
		Timeout: defaultHTTPTimeout,
	}

	klog.InfoS("Starting readiness condition reporter", "node", nodeName, "condition", conditionType, "interval", interval)

	// Main loop to check health and update condition
	for {
		// Check health
		health, err := checkHealth(context.TODO(), httpClient, checkEndpoint)
		if err != nil {
			klog.ErrorS(err, "Health check failed", "endpoint", checkEndpoint)
			// Report unhealthy on error
			health = &HealthResponse{
				Healthy: false,
				Reason:  "HealthCheckFailed",
				Message: fmt.Sprintf("Health check failed: %v", err),
			}
		}

		// Update node condition
		if err := updateNodeCondition(clientset, nodeName, conditionType, health); err != nil {
			klog.ErrorS(err, "Failed to update node condition", "node", nodeName, "condition", conditionType)
		}

		// Wait for next check
		time.Sleep(interval)
	}
}

// checkHealth performs an HTTP request to check component health.
func checkHealth(ctx context.Context, client *http.Client, endpoint string) (*HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return &HealthResponse{
			Healthy: false,
			Reason:  "RequestCreationError",
			Message: fmt.Sprintf("Failed to create request for endpoint %s: %v", endpoint, err),
		}, nil
	}

	resp, err := client.Do(req)
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
func updateNodeCondition(client kubernetes.Interface, nodeName, conditionType string, health *HealthResponse) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get the node
		node, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
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
		for _, condition := range node.Status.Conditions {
			if string(condition.Type) == conditionType {
				if condition.Status == status {
					transitionTime = condition.LastTransitionTime
				}
				break
			}
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

		_, err = client.CoreV1().Nodes().UpdateStatus(context.TODO(), node, metav1.UpdateOptions{})
		return err
	})
}
