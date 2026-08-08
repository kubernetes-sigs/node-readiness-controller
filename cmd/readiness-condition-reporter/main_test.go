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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		name                    string
		existingNode            *corev1.Node
		health                  *HealthResponse
		heartbeatPeriod         time.Duration
		wantStatus              corev1.ConditionStatus
		wantReason              string
		wantUpdateCount         int
		wantTransitionPreserved bool
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
			heartbeatPeriod:         5 * time.Minute,
			wantStatus:              corev1.ConditionTrue,
			wantReason:              "EndpointOK",
			wantUpdateCount:         1,
			wantTransitionPreserved: false,
		},
		{
			// A state change bypasses the heartbeat gate, so the update is written
			// immediately rather than waiting for the next heartbeat.
			name: "update condition on state change",
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
			heartbeatPeriod:         5 * time.Minute,
			wantStatus:              corev1.ConditionFalse,
			wantReason:              "HealthCheckFailed",
			wantUpdateCount:         1,
			wantTransitionPreserved: false,
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
			heartbeatPeriod:         5 * time.Minute,
			wantStatus:              corev1.ConditionTrue,
			wantReason:              "EndpointOk",
			wantUpdateCount:         0,
			wantTransitionPreserved: true,
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
			heartbeatPeriod:         5 * time.Minute,
			wantStatus:              corev1.ConditionTrue,
			wantReason:              "EndpointOk",
			wantUpdateCount:         1,
			wantTransitionPreserved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existingNode)

			countUpdates := func() int {
				count := 0
				for _, a := range client.Actions() {
					if a.GetVerb() == "update" && a.GetSubresource() == "status" && a.GetResource().Resource == "nodes" {
						count++
					}
				}
				return count
			}

			var previousTransition metav1.Time
			for _, cond := range tt.existingNode.Status.Conditions {
				if string(cond.Type) == conditionType {
					previousTransition = cond.LastTransitionTime
					break
				}
			}
			testStart := metav1.NewTime(time.Now())

			err := updateNodeCondition(context.Background(), client, nodeName, conditionType, tt.health, tt.heartbeatPeriod)
			if err != nil {
				t.Errorf("updateNodeCondition() error = %v", err)
			}

			// Assert whether the status write reached the API server
			if got := countUpdates(); got != tt.wantUpdateCount {
				t.Errorf("UpdateStatus called = %v, want %v", got, tt.wantUpdateCount)
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

			if tt.wantTransitionPreserved {
				if !foundCondition.LastTransitionTime.Equal(&previousTransition) {
					t.Errorf("LastTransitionTime = %v, want it preserved as %v", foundCondition.LastTransitionTime, previousTransition)
				}
			} else if foundCondition.LastTransitionTime.Before(&testStart) {
				t.Errorf("LastTransitionTime = %v, want a new transition at or after %v", foundCondition.LastTransitionTime, testStart)
			}
		})
	}
}

func TestUpdateNodeConditionNodeNotFound(t *testing.T) {
	client := fake.NewSimpleClientset()

	health := &HealthResponse{
		Healthy: true,
		Reason:  "EndpointOK",
		Message: "All good",
	}

	err := updateNodeCondition(context.Background(), client, "missing-node", "TestCondition", health, 5*time.Minute)
	if err == nil {
		t.Fatal("updateNodeCondition() error = nil, want a NotFound error")
	}
	if !apierrors.IsNotFound(err) {
		t.Errorf("updateNodeCondition() error = %v, want a NotFound error", err)
	}

	// The lookup failure must abort before any attempt to write the status.
	for _, a := range client.Actions() {
		if a.GetVerb() == "update" {
			t.Error("UpdateStatus was called after the node lookup failed")
		}
	}
}
