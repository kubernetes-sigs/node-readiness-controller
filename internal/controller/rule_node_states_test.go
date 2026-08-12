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
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
	"sigs.k8s.io/node-readiness-controller/internal/metrics"
)

func newTestScheme(tb testing.TB) *runtime.Scheme {
	tb.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		tb.Fatalf("failed to add corev1 to scheme: %v", err)
	}
	if err := readinessv1alpha1.AddToScheme(scheme); err != nil {
		tb.Fatalf("failed to add readinessv1alpha1 to scheme: %v", err)
	}
	return scheme
}

func gpuTaint() corev1.Taint {
	return corev1.Taint{
		Key:    "readiness.k8s.io/gpu-ready",
		Effect: corev1.TaintEffectNoSchedule,
	}
}

func gpuRule() *readinessv1alpha1.NodeReadinessRule {
	return &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{Name: "gpu-ready"},
		Spec: readinessv1alpha1.NodeReadinessRuleSpec{
			NodeSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"gpu": "true"},
			},
			Taint: gpuTaint(),
		},
	}
}

func gpuNode(name string, tainted bool) *corev1.Node {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"gpu": "true"},
		},
	}
	if tainted {
		n.Spec.Taints = []corev1.Taint{gpuTaint()}
	}
	return n
}

func TestListRuleNodeStates_NoRules(t *testing.T) {
	g := NewWithT(t)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleNodeStates(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(BeEmpty())
}

func TestListRuleNodeStates_ZeroMatches(t *testing.T) {
	g := NewWithT(t)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		gpuRule(),
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-node"}},
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleNodeStates(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(Equal(map[string]metrics.RuleNodeCounts{
		"gpu-ready": {Held: 0, Released: 0},
	}))
}

func TestListRuleNodeStates_MixedHeldReleased(t *testing.T) {
	g := NewWithT(t)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		gpuRule(),
		gpuNode("held-1", true),
		gpuNode("held-2", true),
		gpuNode("released-1", false),
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "non-matching"}},
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleNodeStates(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(Equal(map[string]metrics.RuleNodeCounts{
		"gpu-ready": {Held: 2, Released: 1},
	}))
}

func TestListRuleNodeStates_DryRunRuleExcluded(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRule()
	rule.Spec.DryRun = true
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		rule,
		gpuNode("held-1", true),
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleNodeStates(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(BeEmpty())
}

func TestListRuleNodeStates_DeletingRuleIncluded(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRule()
	now := metav1.Now()
	rule.DeletionTimestamp = &now
	rule.Finalizers = []string{"readiness.node.x-k8s.io/cleanup-taints"}
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		rule,
		gpuNode("held-1", true),
		gpuNode("held-2", true),
		gpuNode("released-1", false),
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleNodeStates(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(Equal(map[string]metrics.RuleNodeCounts{
		"gpu-ready": {Held: 2, Released: 1},
	}))
}

func TestListRuleNodeStates_DeletingRulePersistsUntilFinalizer(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRule()
	now := metav1.Now()
	rule.DeletionTimestamp = &now
	rule.Finalizers = []string{"readiness.node.x-k8s.io/cleanup-taints"}
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		rule,
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleNodeStates(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(Equal(map[string]metrics.RuleNodeCounts{
		"gpu-ready": {Held: 0, Released: 0},
	}))
}

func TestListRuleNodeStates_OneRuleHeldOtherReleased(t *testing.T) {
	g := NewWithT(t)
	taintA := corev1.Taint{Key: "readiness.k8s.io/rule-a", Effect: corev1.TaintEffectNoSchedule}
	taintB := corev1.Taint{Key: "readiness.k8s.io/rule-b", Effect: corev1.TaintEffectNoSchedule}

	ruleA := &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule-a"},
		Spec: readinessv1alpha1.NodeReadinessRuleSpec{
			NodeSelector: metav1.LabelSelector{MatchLabels: map[string]string{"gpu": "true"}},
			Taint:        taintA,
		},
	}
	ruleB := &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule-b"},
		Spec: readinessv1alpha1.NodeReadinessRuleSpec{
			NodeSelector: metav1.LabelSelector{MatchLabels: map[string]string{"gpu": "true"}},
			Taint:        taintB,
		},
	}

	// Node matches both rules' selectors but only carries taintA, not taintB.
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "shared-node",
			Labels: map[string]string{"gpu": "true"},
		},
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{taintA},
		},
	}

	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		ruleA, ruleB, node,
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleNodeStates(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(Equal(map[string]metrics.RuleNodeCounts{
		"rule-a": {Held: 1, Released: 0},
		"rule-b": {Held: 0, Released: 1},
	}))
}

func TestListRuleNodeStates_InvalidSelectorSkipped(t *testing.T) {
	g := NewWithT(t)
	validRule := gpuRule()
	invalidRule := &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{Name: "invalid-selector-rule"},
		Spec: readinessv1alpha1.NodeReadinessRuleSpec{
			NodeSelector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "gpu", Operator: "BogusOperator", Values: []string{"true"}},
				},
			},
			Taint: corev1.Taint{
				Key:    "readiness.k8s.io/invalid-selector",
				Effect: corev1.TaintEffectNoSchedule,
			},
		},
	}
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		validRule,
		invalidRule,
		gpuNode("held-1", true),
		gpuNode("released-1", false),
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleNodeStates(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).NotTo(HaveKey(invalidRule.Name))
	g.Expect(counts).To(Equal(map[string]metrics.RuleNodeCounts{
		"gpu-ready": {Held: 1, Released: 1},
	}))
}
