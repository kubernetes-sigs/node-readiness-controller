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

// gpuRuleWithConditions returns a GPU-ready rule with the given conditions.
func gpuRuleWithConditions(conditionTypes ...string) *readinessv1alpha1.NodeReadinessRule {
	rule := gpuRule()
	conditions := make([]readinessv1alpha1.ConditionRequirement, 0, len(conditionTypes))
	for _, ct := range conditionTypes {
		conditions = append(conditions, readinessv1alpha1.ConditionRequirement{
			Type:           ct,
			RequiredStatus: corev1.ConditionTrue,
		})
	}
	rule.Spec.Conditions = conditions
	return rule
}

func gpuRuleWithConditionReqs(reqs ...readinessv1alpha1.ConditionRequirement) *readinessv1alpha1.NodeReadinessRule {
	rule := gpuRule()
	rule.Spec.Conditions = reqs
	return rule
}

func withNodeConditions(node *corev1.Node, conds ...corev1.NodeCondition) *corev1.Node {
	node.Status.Conditions = conds
	return node
}

func nodeCondition(condType string, status corev1.ConditionStatus) corev1.NodeCondition {
	return corev1.NodeCondition{Type: corev1.NodeConditionType(condType), Status: status}
}

func TestListNodes_Empty(t *testing.T) {
	g := NewWithT(t)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	nodes, err := c.ListNodes(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(nodes).To(BeEmpty())
}

func TestListNodes_ReturnsAllCachedNodes(t *testing.T) {
	g := NewWithT(t)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		gpuNode("node-a", true),
		gpuNode("node-b", false),
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	nodes, err := c.ListNodes(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		names = append(names, n.Name)
	}
	g.Expect(names).To(ConsistOf("node-a", "node-b"))
}

func TestListRuleNodeStates_NoRules(t *testing.T) {
	g := NewWithT(t)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleNodeStates(t.Context(), nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(BeEmpty())
}

func TestListRuleNodeStates_ZeroMatches(t *testing.T) {
	g := NewWithT(t)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(gpuRule()).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "cpu-node"}},
	}

	counts, err := c.ListRuleNodeStates(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(Equal(map[string]metrics.RuleNodeCounts{
		"gpu-ready": {Held: 0, Released: 0},
	}))
}

func TestListRuleNodeStates_MixedHeldReleased(t *testing.T) {
	g := NewWithT(t)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(gpuRule()).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*gpuNode("held-1", true),
		*gpuNode("held-2", true),
		*gpuNode("released-1", false),
		{ObjectMeta: metav1.ObjectMeta{Name: "non-matching"}},
	}

	counts, err := c.ListRuleNodeStates(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(Equal(map[string]metrics.RuleNodeCounts{
		"gpu-ready": {Held: 2, Released: 1},
	}))
}

func TestListRuleNodeStates_DryRunRuleExcluded(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRule()
	rule.Spec.DryRun = true
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{*gpuNode("held-1", true)}

	counts, err := c.ListRuleNodeStates(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(BeEmpty())
}

func TestListRuleNodeStates_DeletingRuleIncluded(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRule()
	now := metav1.Now()
	rule.DeletionTimestamp = &now
	rule.Finalizers = []string{"readiness.node.x-k8s.io/cleanup-taints"}
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*gpuNode("held-1", true),
		*gpuNode("held-2", true),
		*gpuNode("released-1", false),
	}

	counts, err := c.ListRuleNodeStates(t.Context(), nodes)
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

	counts, err := c.ListRuleNodeStates(t.Context(), nil)
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
	nodes := []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "shared-node",
				Labels: map[string]string{"gpu": "true"},
			},
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{taintA},
			},
		},
	}

	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		ruleA, ruleB,
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleNodeStates(t.Context(), nodes)
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
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*gpuNode("held-1", true),
		*gpuNode("released-1", false),
	}

	counts, err := c.ListRuleNodeStates(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).NotTo(HaveKey(invalidRule.Name))
	g.Expect(counts).To(Equal(map[string]metrics.RuleNodeCounts{
		"gpu-ready": {Held: 1, Released: 1},
	}))
}

func TestListBlockedNodes_ZeroSeededWhenNoHeldNodes(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditions("GPUDriverReady", "CNIReady")
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {"GPUDriverReady": 0, "CNIReady": 0},
	}))
}

func TestListBlockedNodes_UnsatisfiedConditionCounted(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditions("GPUDriverReady")
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*withNodeConditions(gpuNode("held-1", true), nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {"GPUDriverReady": 1},
	}))
}

func TestListBlockedNodes_ReleasedNodeExcluded(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditions("GPUDriverReady")
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*withNodeConditions(gpuNode("released-1", false), nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {"GPUDriverReady": 0},
	}))
}

func TestListBlockedNodes_MixedHeldReleased(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditions("GPUDriverReady")
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*withNodeConditions(gpuNode("held-1", true), nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
		*withNodeConditions(gpuNode("held-2", true), nodeCondition("GPUDriverReady", corev1.ConditionTrue)),
		*withNodeConditions(gpuNode("released-1", false), nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {"GPUDriverReady": 1},
	}))
}

func TestListBlockedNodes_MultipleUnsatisfiedConditions(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditions("GPUDriverReady", "CNIReady", "DiskReady")
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*withNodeConditions(gpuNode("held-1", true),
			nodeCondition("GPUDriverReady", corev1.ConditionFalse),
			nodeCondition("CNIReady", corev1.ConditionFalse),
			nodeCondition("DiskReady", corev1.ConditionTrue),
		),
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {"GPUDriverReady": 1, "CNIReady": 1, "DiskReady": 0},
	}))
}

// TestListBlockedNodes_AnyOfNoConditionsSatisfied verifies that all conditions are counted when none are satisfied.
func TestListBlockedNodes_AnyOfNoConditionsSatisfied(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditions("GPUDriverReady", "CNIReady")
	rule.Spec.ConditionPolicy = readinessv1alpha1.ConditionPolicyAnyOf
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*withNodeConditions(gpuNode("held-1", true),
			nodeCondition("GPUDriverReady", corev1.ConditionFalse),
			nodeCondition("CNIReady", corev1.ConditionFalse),
		),
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {"GPUDriverReady": 1, "CNIReady": 1},
	}))
}

// TestListBlockedNodes_AnyOfWithSatisfiedCondition verifies that only unsatisfied conditions are counted.
func TestListBlockedNodes_AnyOfWithSatisfiedCondition(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditions("GPUDriverReady", "CNIReady")
	rule.Spec.ConditionPolicy = readinessv1alpha1.ConditionPolicyAnyOf
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*withNodeConditions(gpuNode("held-1", true),
			nodeCondition("GPUDriverReady", corev1.ConditionFalse),
			nodeCondition("CNIReady", corev1.ConditionTrue),
		),
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {"GPUDriverReady": 1, "CNIReady": 0},
	}))
}

// TestListBlockedNodes_SharedConditionAcrossRules verifies that the same condition type is counted independently for each rule.
func TestListBlockedNodes_SharedConditionAcrossRules(t *testing.T) {
	g := NewWithT(t)
	taintA := corev1.Taint{Key: "readiness.k8s.io/rule-a", Effect: corev1.TaintEffectNoSchedule}
	taintB := corev1.Taint{Key: "readiness.k8s.io/rule-b", Effect: corev1.TaintEffectNoSchedule}

	ruleA := &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule-a"},
		Spec: readinessv1alpha1.NodeReadinessRuleSpec{
			NodeSelector: metav1.LabelSelector{MatchLabels: map[string]string{"gpu": "true"}},
			Taint:        taintA,
			Conditions: []readinessv1alpha1.ConditionRequirement{
				{Type: "GPUDriverReady", RequiredStatus: corev1.ConditionTrue},
			},
		},
	}
	ruleB := &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule-b"},
		Spec: readinessv1alpha1.NodeReadinessRuleSpec{
			NodeSelector: metav1.LabelSelector{MatchLabels: map[string]string{"gpu": "true"}},
			Taint:        taintB,
			Conditions: []readinessv1alpha1.ConditionRequirement{
				{Type: "GPUDriverReady", RequiredStatus: corev1.ConditionFalse},
			},
		},
	}

	nodes := []corev1.Node{
		*withNodeConditions(&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "shared-node",
				Labels: map[string]string{"gpu": "true"},
			},
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{taintA, taintB},
			},
		}, nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
	}

	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		ruleA, ruleB,
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"rule-a": {"GPUDriverReady": 1},
		"rule-b": {"GPUDriverReady": 0},
	}))
}

func TestListBlockedNodes_DryRunRuleExcluded(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditions("GPUDriverReady")
	rule.Spec.DryRun = true
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*withNodeConditions(gpuNode("held-1", true), nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(BeEmpty())
}

func TestListBlockedNodes_DeletingRuleExcluded(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditions("GPUDriverReady")
	now := metav1.Now()
	rule.DeletionTimestamp = &now
	rule.Finalizers = []string{"readiness.node.x-k8s.io/cleanup-taints"}
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*withNodeConditions(gpuNode("held-1", true), nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
		*withNodeConditions(gpuNode("held-2", true), nodeCondition("GPUDriverReady", corev1.ConditionTrue)),
		*withNodeConditions(gpuNode("released-1", false), nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(BeEmpty())
}

func TestListBlockedNodes_NonMatchingNodeExcluded(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditions("GPUDriverReady")
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*withNodeConditions(&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "drifted",
				Labels: map[string]string{"gpu": "false"},
			},
			Spec: corev1.NodeSpec{Taints: []corev1.Taint{gpuTaint()}},
		}, nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {"GPUDriverReady": 0},
	}))
}

func TestListBlockedNodes_InvalidSelectorSkipped(t *testing.T) {
	g := NewWithT(t)
	validRule := gpuRuleWithConditions("GPUDriverReady")
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
			Conditions: []readinessv1alpha1.ConditionRequirement{
				{Type: "SomeCondition", RequiredStatus: corev1.ConditionTrue},
			},
		},
	}
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		validRule,
		invalidRule,
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{
		*withNodeConditions(gpuNode("held-1", true), nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).NotTo(HaveKey(invalidRule.Name))
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {"GPUDriverReady": 1},
	}))
}

func TestListBlockedNodes_ZeroDeclaredConditions(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditions()
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}
	nodes := []corev1.Node{*gpuNode("held-1", true)}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {},
	}))
}

func TestListBlockedNodes_NoRules(t *testing.T) {
	g := NewWithT(t)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(BeEmpty())
}

// TestListBlockedNodes_DefaultStatusSatisfies verifies that a default status can satisfy an absent condition.
func TestListBlockedNodes_DefaultStatusSatisfies(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditionReqs(
		readinessv1alpha1.ConditionRequirement{Type: "CondA", RequiredStatus: corev1.ConditionTrue},
		readinessv1alpha1.ConditionRequirement{Type: "CondB", RequiredStatus: corev1.ConditionTrue, DefaultStatus: corev1.ConditionTrue},
	)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	nodes := []corev1.Node{
		*withNodeConditions(gpuNode("held-1", true), nodeCondition("CondA", corev1.ConditionFalse)),
	}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {"CondA": 1, "CondB": 0},
	}))
}

// TestListBlockedNodes_AbsentConditionCounted verifies that an absent condition without a default status is counted as blocking.
func TestListBlockedNodes_AbsentConditionCounted(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditionReqs(
		readinessv1alpha1.ConditionRequirement{Type: "CondB", RequiredStatus: corev1.ConditionTrue},
	)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(rule).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	nodes := []corev1.Node{*gpuNode("held-1", true)}

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {"CondB": 1},
	}))
}

// TestSharedNodeSnapshot_BothListersAgree verifies that both listers use the same Node snapshot.
func TestSharedNodeSnapshot_BothListersAgree(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRuleWithConditions("GPUDriverReady")
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		rule,
		withNodeConditions(gpuNode("held-1", true), nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
		gpuNode("released-1", false),
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	nodes, err := c.ListNodes(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(nodes).To(HaveLen(2))

	ruleCounts, err := c.ListRuleNodeStates(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ruleCounts).To(Equal(map[string]metrics.RuleNodeCounts{
		"gpu-ready": {Held: 1, Released: 1},
	}))

	blocked, err := c.ListBlockedNodes(t.Context(), nodes)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(Equal(map[string]metrics.RuleBlockedConditions{
		"gpu-ready": {"GPUDriverReady": 1},
	}))
}
