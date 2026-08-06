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

package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/events"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

func TestWarningEventsEmittedOnFailures(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = readinessv1alpha1.AddToScheme(scheme)

	rule := &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{Name: "test-warning-rule"},
		Spec: readinessv1alpha1.NodeReadinessRuleSpec{
			Conditions: []readinessv1alpha1.ConditionRequirement{
				{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
			},
			Taint:        corev1.Taint{Key: "readiness.k8s.io/warning-test", Effect: corev1.TaintEffectNoSchedule},
			NodeSelector: metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
		},
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node", Labels: map[string]string{"role": "worker"}},
		Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: "Ready", Status: corev1.ConditionFalse}}},
	}

	// Fake client without the node created so API calls will fail
	fc := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(rule).WithStatusSubresource(rule).Build()
	fakeRecorder := events.NewFakeRecorder(100)

	c := &RuleReadinessController{
		Client:        fc,
		Scheme:        scheme,
		clientset:     fake.NewSimpleClientset(),
		ruleCache:     map[string]*readinessv1alpha1.NodeReadinessRule{rule.Name: rule},
		EventRecorder: fakeRecorder,
	}

	// evaluateRuleForNode with missing node object in Client triggers addTaintBySpec failure
	err := c.evaluateRuleForNode(ctx, rule, node)
	if err == nil {
		t.Fatalf("expected error from evaluateRuleForNode on missing node object, got nil")
	}

	var eventsCaptured []string
	for len(fakeRecorder.Events) > 0 {
		eventsCaptured = append(eventsCaptured, <-fakeRecorder.Events)
	}

	if len(eventsCaptured) == 0 {
		t.Fatalf("expected warning events to be recorded, but fakeRecorder was empty")
	}

	hasAddTaintError := false
	hasWarning := false
	for _, evt := range eventsCaptured {
		if containsString(evt, "AddTaintError") {
			hasAddTaintError = true
		}
		if containsString(evt, "Warning") {
			hasWarning = true
		}
	}

	if !hasAddTaintError || !hasWarning {
		t.Fatalf("expected Warning event with reason AddTaintError, got events: %v", eventsCaptured)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
