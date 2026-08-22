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
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

type failingStatusWriter struct {
	client.StatusWriter
	patchError error
}

func (sw *failingStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	if sw.patchError != nil {
		return sw.patchError
	}
	return sw.StatusWriter.Patch(ctx, obj, patch, opts...)
}

type failingClient struct {
	client.Client
	listError        error
	statusPatchError error
}

func (c *failingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if c.listError != nil {
		return c.listError
	}
	return c.Client.List(ctx, list, opts...)
}

func (c *failingClient) Status() client.StatusWriter {
	return &failingStatusWriter{
		StatusWriter: c.Client.Status(),
		patchError:   c.statusPatchError,
	}
}

func TestWarningEventsEmittedOnFailures(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = readinessv1alpha1.AddToScheme(scheme)

	rule := &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-warning-rule",
			Finalizers: []string{finalizerName},
		},
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

	fc := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(rule).WithStatusSubresource(rule).Build()
	fakeRecorder := events.NewFakeRecorder(100)

	c := &RuleReadinessController{
		Client:        fc,
		Scheme:        scheme,
		clientset:     fake.NewSimpleClientset(),
		ruleCache:     map[string]*readinessv1alpha1.NodeReadinessRule{rule.Name: rule},
		EventRecorder: fakeRecorder,
	}

	if err := c.evaluateRuleForNode(ctx, rule, node); err == nil {
		t.Fatalf("expected error, got nil")
	}

	var eventsCaptured []string
	for len(fakeRecorder.Events) > 0 {
		eventsCaptured = append(eventsCaptured, <-fakeRecorder.Events)
	}

	if len(eventsCaptured) != 1 {
		t.Fatalf("expected exactly 1 event, got: %v", eventsCaptured)
	}

	if !strings.Contains(eventsCaptured[0], "AddTaintError") || !strings.Contains(eventsCaptured[0], "Warning") {
		t.Fatalf("expected Warning AddTaintError event, got: %v", eventsCaptured[0])
	}
}

func TestReconcile_ListError(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = readinessv1alpha1.AddToScheme(scheme)

	rule := &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-warning-rule",
			Finalizers: []string{finalizerName},
		},
		Spec: readinessv1alpha1.NodeReadinessRuleSpec{
			Conditions: []readinessv1alpha1.ConditionRequirement{
				{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
			},
			Taint:        corev1.Taint{Key: "readiness.k8s.io/warning-test", Effect: corev1.TaintEffectNoSchedule},
			NodeSelector: metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
		},
	}

	baseClient := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(rule).WithStatusSubresource(rule).Build()
	fc := &failingClient{
		Client:    baseClient,
		listError: fmt.Errorf("fake listing error"),
	}
	fakeRecorder := events.NewFakeRecorder(100)

	c := &RuleReadinessController{
		Client:        fc,
		Scheme:        scheme,
		clientset:     fake.NewSimpleClientset(),
		ruleCache:     map[string]*readinessv1alpha1.NodeReadinessRule{rule.Name: rule},
		EventRecorder: fakeRecorder,
	}

	r := &RuleReconciler{
		Client:     fc,
		Scheme:     scheme,
		Controller: c,
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: rule.Name}}); err == nil {
		t.Fatalf("expected error, got nil")
	}

	var eventsCaptured []string
	for len(fakeRecorder.Events) > 0 {
		eventsCaptured = append(eventsCaptured, <-fakeRecorder.Events)
	}

	hasListError := false
	for _, evt := range eventsCaptured {
		if strings.Contains(evt, "ListNodesError") && strings.Contains(evt, "Warning") {
			hasListError = true
		}
	}
	if !hasListError {
		t.Fatalf("expected Warning ListNodesError event, got: %v", eventsCaptured)
	}
}

func TestReconcile_StatusPatchError(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = readinessv1alpha1.AddToScheme(scheme)

	rule := &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-warning-rule",
			Finalizers: []string{finalizerName},
		},
		Spec: readinessv1alpha1.NodeReadinessRuleSpec{
			Conditions: []readinessv1alpha1.ConditionRequirement{
				{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
			},
			Taint:        corev1.Taint{Key: "readiness.k8s.io/warning-test", Effect: corev1.TaintEffectNoSchedule},
			NodeSelector: metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
		},
	}

	baseClient := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(rule).WithStatusSubresource(rule).Build()
	fc := &failingClient{
		Client:           baseClient,
		statusPatchError: fmt.Errorf("fake status patch error"),
	}
	fakeRecorder := events.NewFakeRecorder(100)

	c := &RuleReadinessController{
		Client:        fc,
		Scheme:        scheme,
		clientset:     fake.NewSimpleClientset(),
		ruleCache:     map[string]*readinessv1alpha1.NodeReadinessRule{rule.Name: rule},
		EventRecorder: fakeRecorder,
	}

	r := &RuleReconciler{
		Client:     fc,
		Scheme:     scheme,
		Controller: c,
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: rule.Name}}); err == nil {
		t.Fatalf("expected error, got nil")
	}

	var eventsCaptured []string
	for len(fakeRecorder.Events) > 0 {
		eventsCaptured = append(eventsCaptured, <-fakeRecorder.Events)
	}

	hasStatusError := false
	for _, evt := range eventsCaptured {
		if strings.Contains(evt, "StatusUpdateError") && strings.Contains(evt, "Warning") {
			hasStatusError = true
		}
	}
	if !hasStatusError {
		t.Fatalf("expected Warning StatusUpdateError event, got: %v", eventsCaptured)
	}
}
