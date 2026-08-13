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
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"sigs.k8s.io/node-readiness-controller/internal/info"
)

func TestReporterBuildInfo(t *testing.T) {
	expected := `
# HELP node_readiness_reporter_build_info Reporter binary version.
# TYPE node_readiness_reporter_build_info gauge
node_readiness_reporter_build_info{version="` + info.GetVersion() + `"} 1
`
	if err := testutil.CollectAndCompare(reporterBuildInfo, strings.NewReader(expected), "node_readiness_reporter_build_info"); err != nil {
		t.Fatalf("unexpected collecting result:\n%s", err)
	}

	gathered, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var found bool
	for _, mf := range gathered {
		if mf.GetName() == "node_readiness_reporter_build_info" {
			found = true
			if got := mf.GetType().String(); got != "GAUGE" {
				t.Fatalf("expected node_readiness_reporter_build_info to be a gauge, got %s", got)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected node_readiness_reporter_build_info to be registered with the reporter metrics registry")
	}
}

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

func TestReporterMetricsCollectAndLint(t *testing.T) {
	metrics := []struct {
		name string
		col  prometheus.Collector
	}{
		{"reporterBuildInfo", reporterBuildInfo},
		{"reporterCheckDuration", reporterCheckDuration},
		{"reporterChecksTotal", reporterChecksTotal},
		{"reporterConditionWritesTotal", reporterConditionWritesTotal},
	}

	for _, m := range metrics {
		t.Run(m.name, func(t *testing.T) {
			problems, err := testutil.CollectAndLint(m.col)
			if err != nil {
				t.Fatalf("CollectAndLint(%s) error = %v", m.name, err)
			}
			if len(problems) > 0 {
				t.Errorf("CollectAndLint(%s) found problems: %+v", m.name, problems)
			}
		})
	}
}

func TestRunCheckMetrics(t *testing.T) {
	nodeName := "metrics-test-node"
	conditionType := "TestCondition"

	tests := []struct {
		name           string
		status         int
		wantCheckLabel string
	}{
		{name: "Healthy", status: http.StatusOK, wantCheckLabel: "healthy"},
		{name: "Unhealthy", status: http.StatusInternalServerError, wantCheckLabel: "unhealthy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			client := fake.NewSimpleClientset(node)
			httpClient := &http.Client{Timeout: 1 * time.Second}

			before := testutil.ToFloat64(reporterChecksTotal.WithLabelValues(tt.wantCheckLabel))
			beforeWrites := testutil.ToFloat64(reporterConditionWritesTotal.WithLabelValues("success"))

			runCheck(context.Background(), httpClient, client, server.URL, nodeName, conditionType, 5*time.Minute)

			after := testutil.ToFloat64(reporterChecksTotal.WithLabelValues(tt.wantCheckLabel))
			if after != before+1 {
				t.Errorf("reporterChecksTotal{result=%q} = %v, want %v", tt.wantCheckLabel, after, before+1)
			}

			afterWrites := testutil.ToFloat64(reporterConditionWritesTotal.WithLabelValues("success"))
			if afterWrites != beforeWrites+1 {
				t.Errorf("reporterConditionWritesTotal{result=\"success\"} = %v, want %v", afterWrites, beforeWrites+1)
			}
		})
	}
}

func TestRunCheckMetricsError(t *testing.T) {
	nodeName := "metrics-error-node"
	conditionType := "TestCondition"

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
	client := fake.NewSimpleClientset(node)
	httpClient := &http.Client{Timeout: 1 * time.Second}

	before := testutil.ToFloat64(reporterChecksTotal.WithLabelValues("error"))

	runCheck(context.Background(), httpClient, client, "http://127.0.0.1:0", nodeName, conditionType, 5*time.Minute)

	after := testutil.ToFloat64(reporterChecksTotal.WithLabelValues("error"))
	if after != before+1 {
		t.Errorf("reporterChecksTotal{result=\"error\"} = %v, want %v", after, before+1)
	}
}

func TestRunCheckConditionWriteErrorMetric(t *testing.T) {
	nodeName := "no-such-node"
	conditionType := "TestCondition"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// No node is seeded, so the condition update fails.
	client := fake.NewSimpleClientset()
	httpClient := &http.Client{Timeout: 1 * time.Second}

	before := testutil.ToFloat64(reporterConditionWritesTotal.WithLabelValues("error"))

	runCheck(context.Background(), httpClient, client, server.URL, nodeName, conditionType, 5*time.Minute)

	after := testutil.ToFloat64(reporterConditionWritesTotal.WithLabelValues("error"))
	if after != before+1 {
		t.Errorf("reporterConditionWritesTotal{result=\"error\"} = %v, want %v", after, before+1)
	}
}

func TestUpdateNodeConditionMetrics(t *testing.T) {
	nodeName := "condition-metrics-node"
	conditionType := "TestCondition"
	staleTime := time.Now().Add(-6 * time.Minute)

	t.Run("skipped", func(t *testing.T) {
		existingNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:               corev1.NodeConditionType(conditionType),
						Status:             corev1.ConditionTrue,
						Reason:             "EndpointOK",
						Message:            "All good",
						LastHeartbeatTime:  metav1.NewTime(time.Now()),
						LastTransitionTime: metav1.NewTime(time.Now()),
					},
				},
			},
		}
		client := fake.NewSimpleClientset(existingNode)
		health := &HealthResponse{Healthy: true, Reason: "EndpointOK", Message: "All good"}

		before := testutil.ToFloat64(reporterConditionWritesTotal.WithLabelValues("skipped"))
		if err := updateNodeCondition(context.Background(), client, nodeName, conditionType, health, 5*time.Minute); err != nil {
			t.Fatalf("updateNodeCondition() error = %v", err)
		}
		after := testutil.ToFloat64(reporterConditionWritesTotal.WithLabelValues("skipped"))
		if after != before+1 {
			t.Errorf("reporterConditionWritesTotal{result=\"skipped\"} = %v, want %v", after, before+1)
		}
	})

	t.Run("success on stale heartbeat", func(t *testing.T) {
		existingNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:               corev1.NodeConditionType(conditionType),
						Status:             corev1.ConditionTrue,
						Reason:             "EndpointOK",
						Message:            "All good",
						LastHeartbeatTime:  metav1.NewTime(staleTime),
						LastTransitionTime: metav1.NewTime(staleTime),
					},
				},
			},
		}
		client := fake.NewSimpleClientset(existingNode)
		health := &HealthResponse{Healthy: true, Reason: "EndpointOK", Message: "All good"}

		before := testutil.ToFloat64(reporterConditionWritesTotal.WithLabelValues("success"))
		if err := updateNodeCondition(context.Background(), client, nodeName, conditionType, health, 5*time.Minute); err != nil {
			t.Fatalf("updateNodeCondition() error = %v", err)
		}
		after := testutil.ToFloat64(reporterConditionWritesTotal.WithLabelValues("success"))
		if after != before+1 {
			t.Errorf("reporterConditionWritesTotal{result=\"success\"} = %v, want %v", after, before+1)
		}
	})

	t.Run("error when node missing", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		health := &HealthResponse{Healthy: true, Reason: "EndpointOK", Message: "All good"}

		before := testutil.ToFloat64(reporterConditionWritesTotal.WithLabelValues("success"))
		err := updateNodeCondition(context.Background(), client, "missing-node", conditionType, health, 5*time.Minute)
		if err == nil {
			t.Fatal("updateNodeCondition() expected error for missing node, got nil")
		}
		after := testutil.ToFloat64(reporterConditionWritesTotal.WithLabelValues("success"))
		if after != before {
			t.Errorf("reporterConditionWritesTotal{result=\"success\"} changed on error path: before=%v after=%v", before, after)
		}
	})
}

func TestUpdateNodeCondition(t *testing.T) {
	nodeName := "test-node"
	conditionType := "TestCondition"
	staleTime := time.Now().Add(-6 * time.Minute)

	countUpdateCalls := func(client *fake.Clientset) int {
		calls := 0
		for _, a := range client.Actions() {
			if a.GetVerb() == "update" && a.GetSubresource() == "status" && a.GetResource().Resource == "nodes" {
				calls++
			}
		}
		return calls
	}

	tests := []struct {
		name                    string
		existingNode            *corev1.Node
		health                  *HealthResponse
		heartbeatPeriod         time.Duration
		wantStatus              corev1.ConditionStatus
		wantReason              string
		wantUpdateCount         int
		wantTransitionPreserved bool
		wantNotFoundErr         bool
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
		{
			name:         "Node not found",
			existingNode: nil,
			health: &HealthResponse{
				Healthy: true,
				Reason:  "EndpointOK",
				Message: "All good",
			},
			heartbeatPeriod: 5 * time.Minute,
			wantUpdateCount: 0,
			wantNotFoundErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *fake.Clientset
			if tt.existingNode != nil {
				client = fake.NewClientset(tt.existingNode)
			} else {
				client = fake.NewClientset()
			}

			var previousTransition metav1.Time
			if tt.existingNode != nil {
				for _, cond := range tt.existingNode.Status.Conditions {
					if string(cond.Type) == conditionType {
						previousTransition = cond.LastTransitionTime
						break
					}
				}
			}
			if previousTransition.IsZero() {
				previousTransition = metav1.NewTime(time.Now())
			}

			err := updateNodeCondition(context.Background(), client, nodeName, conditionType, tt.health, tt.heartbeatPeriod)
			if err != nil {
				if tt.wantNotFoundErr {
					if !apierrors.IsNotFound(err) {
						t.Fatalf("updateNodeCondition() error = %v, want a NotFound error", err)
					}
					if got := countUpdateCalls(client); got != tt.wantUpdateCount {
						t.Errorf("UpdateStatus called = %v, want %v", got, tt.wantUpdateCount)
					}
					return
				}
				t.Fatalf("updateNodeCondition() error = %v", err)
			}
			if tt.wantNotFoundErr {
				t.Fatal("updateNodeCondition() succeeded, want a NotFound error")
			}

			// Assert whether the status write reached the API server
			if got := countUpdateCalls(client); got != tt.wantUpdateCount {
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
			} else if !foundCondition.LastTransitionTime.After(previousTransition.Time) {
				t.Errorf("LastTransitionTime = %v, want a new transition at or after %v", foundCondition.LastTransitionTime, previousTransition.Time)
			}
		})
	}
}

func TestParseDurationWithDefault(t *testing.T) {
	defaultVal := 30 * time.Second

	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{
			name:     "valid positive duration",
			input:    "60s",
			expected: 60 * time.Second,
		},
		{
			name:     "zero duration",
			input:    "0s",
			expected: defaultVal,
		},
		{
			name:     "negative duration",
			input:    "-30s",
			expected: defaultVal,
		},
		{
			name:     "unparseable duration",
			input:    "abc",
			expected: defaultVal,
		},
		{
			name:     "empty duration",
			input:    "",
			expected: defaultVal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDurationWithDefault(tt.input, defaultVal, "test duration")
			if result != tt.expected {
				t.Errorf("parseDurationWithDefault(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}
