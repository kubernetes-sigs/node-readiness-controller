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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckHealth(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		responseBody string
		wantHealthy  bool
		wantReason   string
		expectError  bool
	}{
		{
			name:        "Healthy",
			status:      http.StatusOK,
			wantHealthy: true,
			wantReason:  "EndpointOK",
		},
		{
			name:         "Unhealthy Status",
			status:       http.StatusInternalServerError,
			responseBody: "Internal Server Error",
			wantHealthy:  false,
			wantReason:   "EndpointNotReady",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			endpoint := server.URL
			if tt.expectError {
				endpoint = "http://invalid-url"
			}

			httpClient := &http.Client{Timeout: 1 * time.Second}
			health, err := checkHealth(context.Background(), httpClient, endpoint)
			if err != nil {
				if !tt.expectError {
					t.Errorf("checkHealth() error = %v", err)
				}
			}

			if health.Healthy != tt.wantHealthy {
				t.Errorf("checkHealth() healthy = %v, want %v", health.Healthy, tt.wantHealthy)
			}
			if health.Reason != tt.wantReason {
				t.Errorf("checkHealth() reason = %v, want %v", health.Reason, tt.wantReason)
			}
		})
	}
}

func TestCheckHealthCancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	httpClient := &http.Client{Timeout: 1 * time.Second}
	health, err := checkHealth(ctx, httpClient, server.URL)
	// checkHealth wraps connection errors into a HealthResponse rather than returning an error
	if err != nil {
		t.Fatalf("checkHealth() returned unexpected error: %v", err)
	}
	if health.Healthy {
		t.Error("checkHealth() with cancelled context should report unhealthy")
	}
	if health.Reason != "EndpointConnectionError" {
		t.Errorf("checkHealth() reason = %v, want EndpointConnectionError", health.Reason)
	}
}

func TestUpdateNodeCondition(t *testing.T) {
	nodeName := "test-node"
	conditionType := "TestCondition"
	staleTime := time.Now().Add(-6 * time.Minute)

	tests := []struct {
		name             string
		existingNode     *corev1.Node
		health           *HealthResponse
		heartbeatPeriod  time.Duration
		wantStatus       corev1.ConditionStatus
		wantReason       string
		wantUpdateCalled bool
	}{
		{
			name: "New Condition Healthy",
			existingNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			},
			health: &HealthResponse{
				Healthy: true,
				Reason:  "EndpointOK",
				Message: "All good",
			},
			heartbeatPeriod:  5 * time.Minute,
			wantStatus:       corev1.ConditionTrue,
			wantReason:       "EndpointOK",
			wantUpdateCalled: true,
		},
		{
			name: "State change triggers immediate write",
			existingNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeConditionType(conditionType),
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			health: &HealthResponse{
				Healthy: false,
				Reason:  "HealthCheckFailed",
				Message: "Something failed",
			},
			heartbeatPeriod:  5 * time.Minute,
			wantStatus:       corev1.ConditionFalse,
			wantReason:       "HealthCheckFailed",
			wantUpdateCalled: true,
		},
		{
			name: "State unchanged: Fresh heartbeat (skip write)",
			existingNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeConditionType(conditionType),
							Status:             corev1.ConditionTrue,
							Reason:             "EndpointOk",
							Message:            "All good",
							LastHeartbeatTime:  metav1.NewTime(time.Now()),
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
			},
			health: &HealthResponse{
				Healthy: true,
				Reason:  "EndpointOk",
				Message: "All good",
			},
			heartbeatPeriod:  5 * time.Minute,
			wantStatus:       corev1.ConditionTrue,
			wantReason:       "EndpointOk",
			wantUpdateCalled: false,
		},
		{
			name: "State unchanged: Stale heartbeat (force write)",
			existingNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeConditionType(conditionType),
							Status:             corev1.ConditionTrue,
							Reason:             "EndpointOk",
							Message:            "All good",
							LastHeartbeatTime:  metav1.NewTime(staleTime),
							LastTransitionTime: metav1.NewTime(staleTime),
						},
					},
				},
			},
			health: &HealthResponse{
				Healthy: true,
				Reason:  "EndpointOk",
				Message: "All good",
			},
			heartbeatPeriod:  5 * time.Minute,
			wantStatus:       corev1.ConditionTrue,
			wantReason:       "EndpointOk",
			wantUpdateCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existingNode)

			countUpdates := func() int {
				n := 0
				for _, a := range client.Actions() {
					if a.GetVerb() == "update" && a.GetSubresource() == "status" && a.GetResource().Resource == "nodes" {
						n++
					}
				}
				return n
			}

			err := updateNodeCondition(context.Background(), client, nodeName, conditionType, tt.health, tt.heartbeatPeriod)
			if err != nil {
				t.Errorf("updateNodeCondition() error = %v", err)
			}

			// Assert API call frequency
			updateCount := countUpdates()
			if tt.wantUpdateCalled && updateCount == 0 {
				t.Errorf("Expected UpdateStatus to be called, but it was skipped")
			} else if !tt.wantUpdateCalled && updateCount > 0 {
				t.Errorf("Expected UpdateStatus to be skipped, but it was called %d times", updateCount)
			}

			updatedNode, err := client.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get node: %v", err)
			}

			var foundCondition *corev1.NodeCondition
			for _, cond := range updatedNode.Status.Conditions {
				if string(cond.Type) == conditionType {
					foundCondition = &cond
					break
				}
			}

			if foundCondition == nil {
				t.Fatal("Condition not found")
			}

			if foundCondition.Status != tt.wantStatus {
				t.Errorf("Condition status = %v, want %v", foundCondition.Status, tt.wantStatus)
			}
			if foundCondition.Reason != tt.wantReason {
				t.Errorf("Condition reason = %v, want %v", foundCondition.Reason, tt.wantReason)
			}
		})
	}
}
