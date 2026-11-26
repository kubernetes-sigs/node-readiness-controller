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
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
	"sigs.k8s.io/node-readiness-controller/internal/metrics"
)

func TestBootstrapAnnotationKey(t *testing.T) {
	g := NewWithT(t)

	uid := types.UID("550e8400-e29b-41d4-a716-446655440000")
	key := bootstrapAnnotationKey(uid)
	g.Expect(key).To(Equal("readiness.k8s.io/bootstrap-completed-550e8400-e29b-41d4-a716-446655440000"))
}

func TestBootstrapAnnotationValue(t *testing.T) {
	g := NewWithT(t)

	t.Run("encodes rule name as JSON", func(t *testing.T) {
		val := bootstrapAnnotationValue("my-rule")
		g.Expect(val).To(Equal(`{"rule-name":"my-rule"}`))
	})

	t.Run("handles long rule names", func(t *testing.T) {
		longName := "my-very-long-rule-name-that-exceeds-the-63-character-annotation-key-limit-strictly"
		val := bootstrapAnnotationValue(longName)
		g.Expect(val).To(ContainSubstring(longName))
	})
}

func TestLegacyBootstrapAnnotationKey(t *testing.T) {
	g := NewWithT(t)

	key := legacyBootstrapAnnotationKey("my-rule")
	g.Expect(key).To(Equal("readiness.k8s.io/bootstrap-completed-my-rule"))
}

func TestTaintsEqual(t *testing.T) {
	taint := func(key, value string, effect corev1.TaintEffect) corev1.Taint {
		return corev1.Taint{Key: key, Value: value, Effect: effect}
	}

	tests := []struct {
		name     string
		a        []corev1.Taint
		b        []corev1.Taint
		expected bool
	}{
		{
			name:     "both empty",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "identical single taint",
			a:        []corev1.Taint{taint("k", "v", corev1.TaintEffectNoSchedule)},
			b:        []corev1.Taint{taint("k", "v", corev1.TaintEffectNoSchedule)},
			expected: true,
		},
		{
			name:     "order independent",
			a:        []corev1.Taint{taint("k1", "v1", corev1.TaintEffectNoSchedule), taint("k2", "v2", corev1.TaintEffectNoExecute)},
			b:        []corev1.Taint{taint("k2", "v2", corev1.TaintEffectNoExecute), taint("k1", "v1", corev1.TaintEffectNoSchedule)},
			expected: true,
		},
		{
			name:     "different value",
			a:        []corev1.Taint{taint("k", "v1", corev1.TaintEffectNoSchedule)},
			b:        []corev1.Taint{taint("k", "v2", corev1.TaintEffectNoSchedule)},
			expected: false,
		},
		{
			name:     "different effect",
			a:        []corev1.Taint{taint("k", "v", corev1.TaintEffectNoSchedule)},
			b:        []corev1.Taint{taint("k", "v", corev1.TaintEffectNoExecute)},
			expected: false,
		},
		{
			name:     "different length",
			a:        []corev1.Taint{taint("k", "v", corev1.TaintEffectNoSchedule)},
			b:        []corev1.Taint{taint("k", "v", corev1.TaintEffectNoSchedule), taint("k2", "v2", corev1.TaintEffectNoSchedule)},
			expected: false,
		},
		{
			// Regression for issue #400: Key+Effect concatenation collides.
			// "a"+"PreferNoSchedule" == "aPrefer"+"NoSchedule" as strings,
			// but they are different taints and must compare unequal.
			name:     "key-effect concatenation collision",
			a:        []corev1.Taint{taint("a", "v", "PreferNoSchedule")},
			b:        []corev1.Taint{taint("aPrefer", "v", "NoSchedule")},
			expected: false,
		},
		{
			// Map-based implementations collapse duplicates: [X,X] vs [X,Y]
			// has the same length but different multisets.
			name: "duplicate collapse distinguished",
			a: []corev1.Taint{
				taint("k", "v1", corev1.TaintEffectNoSchedule),
				taint("k", "v1", corev1.TaintEffectNoSchedule),
			},
			b: []corev1.Taint{
				taint("k", "v1", corev1.TaintEffectNoSchedule),
				taint("k", "v2", corev1.TaintEffectNoSchedule),
			},
			expected: false,
		},
		{
			name: "duplicate taints equal",
			a: []corev1.Taint{
				taint("k", "v1", corev1.TaintEffectNoSchedule),
				taint("k", "v1", corev1.TaintEffectNoSchedule),
			},
			b: []corev1.Taint{
				taint("k", "v1", corev1.TaintEffectNoSchedule),
				taint("k", "v1", corev1.TaintEffectNoSchedule),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(taintsEqual(tt.a, tt.b)).To(Equal(tt.expected))
			g.Expect(taintsEqual(tt.b, tt.a)).To(Equal(tt.expected))
		})
	}

	t.Run("ignores TimeAdded", func(t *testing.T) {
		g := NewWithT(t)
		a := []corev1.Taint{{Key: "k", Value: "v", Effect: corev1.TaintEffectNoSchedule, TimeAdded: &metav1.Time{}}}
		b := []corev1.Taint{{Key: "k", Value: "v", Effect: corev1.TaintEffectNoSchedule}}
		g.Expect(taintsEqual(a, b)).To(BeTrue())
	})
}

func TestLabelsEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        map[string]string
		b        map[string]string
		expected bool
	}{
		{
			name:     "identical labels",
			a:        map[string]string{"env": "prod"},
			b:        map[string]string{"env": "prod"},
			expected: true,
		},
		{
			name:     "different value for the same key",
			a:        map[string]string{"env": "prod"},
			b:        map[string]string{"env": "staging"},
			expected: false,
		},
		{
			name:     "extra key",
			a:        map[string]string{"env": "prod"},
			b:        map[string]string{"env": "prod", "tier": "frontend"},
			expected: false,
		},
		{
			// Role labels conventionally carry an empty value, so a swap between
			// two of them differs only by key.
			name:     "empty valued role label swapped for another",
			a:        map[string]string{"node-role.kubernetes.io/worker": ""},
			b:        map[string]string{"node-role.kubernetes.io/infra": ""},
			expected: false,
		},
		{
			name:     "disjoint empty valued keys alongside a shared label",
			a:        map[string]string{"a": "", "shared": "x"},
			b:        map[string]string{"b": "", "shared": "x"},
			expected: false,
		},
		{
			name:     "empty valued label kept unchanged",
			a:        map[string]string{"node-role.kubernetes.io/worker": ""},
			b:        map[string]string{"node-role.kubernetes.io/worker": ""},
			expected: true,
		},
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "nil compared to empty valued label",
			a:        nil,
			b:        map[string]string{"node-role.kubernetes.io/worker": ""},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(labelsEqual(tt.a, tt.b)).To(Equal(tt.expected))
			// The comparison must be symmetric.
			g.Expect(labelsEqual(tt.b, tt.a)).To(Equal(tt.expected))
		})
	}
}

func TestNodeStatusRelevantChange(t *testing.T) {
	base := func() *corev1.Node {
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "n",
				Labels:      map[string]string{"env": "prod"},
				Annotations: map[string]string{"unrelated": "a"},
			},
			Spec: corev1.NodeSpec{Taints: []corev1.Taint{{Key: "k", Effect: corev1.TaintEffectNoSchedule}}},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: "Ready", Status: corev1.ConditionTrue}},
			},
		}
	}

	tests := []struct {
		name     string
		mutate   func(n *corev1.Node)
		expected bool
	}{
		{"no change", func(n *corev1.Node) {}, false},
		{"unrelated annotation", func(n *corev1.Node) { n.Annotations["unrelated"] = "b" }, false},
		{"heartbeat only", func(n *corev1.Node) { n.Status.Conditions[0].LastHeartbeatTime = metav1.Now() }, false},
		{"condition status", func(n *corev1.Node) { n.Status.Conditions[0].Status = corev1.ConditionFalse }, true},
		{"taint removed", func(n *corev1.Node) { n.Spec.Taints = nil }, true},
		{"label changed", func(n *corev1.Node) { n.Labels["env"] = "dev" }, true},
		{"bootstrap annotation added", func(n *corev1.Node) {
			n.Annotations[bootstrapAnnotationKey("uid-1")] = `{"rule-name":"r"}`
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			updated := base()
			tt.mutate(updated)
			g.Expect(nodeStatusRelevantChange(base(), updated)).To(Equal(tt.expected))
		})
	}
}

func TestEvaluateRuleConditions(t *testing.T) {
	c := &RuleReadinessController{}
	node := &corev1.Node{
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: "A", Status: corev1.ConditionTrue},
				{Type: "B", Status: corev1.ConditionFalse},
			},
		},
	}
	newRule := func(policy readinessv1alpha1.ConditionPolicy, condTypes ...string) *readinessv1alpha1.NodeReadinessRule {
		rule := &readinessv1alpha1.NodeReadinessRule{
			Spec: readinessv1alpha1.NodeReadinessRuleSpec{ConditionPolicy: policy},
		}
		for _, ct := range condTypes {
			rule.Spec.Conditions = append(rule.Spec.Conditions, readinessv1alpha1.ConditionRequirement{
				Type: ct, RequiredStatus: corev1.ConditionTrue,
			})
		}
		return rule
	}

	t.Run("allOf requires every condition", func(t *testing.T) {
		g := NewWithT(t)
		outcomes, satisfied := c.evaluateRuleConditions(node, newRule(readinessv1alpha1.ConditionPolicyAllOf, "A", "B"))
		g.Expect(satisfied).To(BeFalse())
		g.Expect(outcomes).To(HaveLen(2))
		g.Expect(outcomes[0].satisfied).To(BeTrue())
		g.Expect(outcomes[1].satisfied).To(BeFalse())
	})

	t.Run("anyOf requires one condition", func(t *testing.T) {
		g := NewWithT(t)
		_, satisfied := c.evaluateRuleConditions(node, newRule(readinessv1alpha1.ConditionPolicyAnyOf, "A", "B"))
		g.Expect(satisfied).To(BeTrue())
	})

	t.Run("missing condition is reported Unknown and unsatisfied", func(t *testing.T) {
		g := NewWithT(t)
		outcomes, satisfied := c.evaluateRuleConditions(node, newRule(readinessv1alpha1.ConditionPolicyAllOf, "Missing"))
		g.Expect(satisfied).To(BeFalse())
		g.Expect(outcomes[0].result.CurrentStatus).To(Equal(corev1.ConditionUnknown))
	})
}

func TestDeriveRuleStatus(t *testing.T) {
	c := &RuleReadinessController{}
	taint := corev1.Taint{Key: "readiness.k8s.io/t", Effect: corev1.TaintEffectNoSchedule}

	nodeList := &corev1.NodeList{Items: []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "b-tainted", Labels: map[string]string{"env": "prod"}},
			Spec:       corev1.NodeSpec{Taints: []corev1.Taint{taint}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "a-ready", Labels: map[string]string{"env": "prod"}},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: "Ready", Status: corev1.ConditionTrue}},
			},
		},
		{ObjectMeta: metav1.ObjectMeta{Name: "c-other", Labels: map[string]string{"env": "dev"}}},
	}}

	newRule := func() *readinessv1alpha1.NodeReadinessRule {
		return &readinessv1alpha1.NodeReadinessRule{
			ObjectMeta: metav1.ObjectMeta{Name: "r", Generation: 3},
			Spec: readinessv1alpha1.NodeReadinessRuleSpec{
				Conditions:      []readinessv1alpha1.ConditionRequirement{{Type: "Ready", RequiredStatus: corev1.ConditionTrue}},
				NodeSelector:    metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
				Taint:           taint,
				EnforcementMode: readinessv1alpha1.EnforcementModeContinuous,
			},
			Status: readinessv1alpha1.NodeReadinessRuleStatus{
				FailedNodes: []readinessv1alpha1.NodeFailure{{NodeName: "b-tainted"}},
			},
		}
	}

	t.Run("derives sorted per-node status from node state", func(t *testing.T) {
		g := NewWithT(t)
		rule := newRule()
		c.deriveRuleStatus(rule, nodeList)

		g.Expect(rule.Status.ObservedGeneration).To(Equal(int64(3)))
		g.Expect(rule.Status.AppliedNodes).To(Equal([]string{"a-ready", "b-tainted"}))
		g.Expect(rule.Status.NodeEvaluations).To(HaveLen(2))
		g.Expect(rule.Status.NodeEvaluations[0].NodeName).To(Equal("a-ready"))
		g.Expect(rule.Status.NodeEvaluations[0].TaintStatus).To(Equal(readinessv1alpha1.TaintStatusAbsent))
		g.Expect(rule.Status.NodeEvaluations[1].NodeName).To(Equal("b-tainted"))
		g.Expect(rule.Status.NodeEvaluations[1].TaintStatus).To(Equal(readinessv1alpha1.TaintStatusPresent))
		g.Expect(rule.Status.NodeEvaluations[1].ConditionResults[0].CurrentStatus).To(Equal(corev1.ConditionUnknown))
		g.Expect(rule.Status.FailedNodes).To(BeEmpty())
	})

	t.Run("unchanged node keeps its lastEvaluationTime", func(t *testing.T) {
		g := NewWithT(t)
		rule := newRule()
		c.deriveRuleStatus(rule, nodeList)
		first := rule.Status.DeepCopy()

		c.deriveRuleStatus(rule, nodeList)
		g.Expect(rule.Status).To(Equal(*first))
	})

	t.Run("changed taint refreshes lastEvaluationTime", func(t *testing.T) {
		g := NewWithT(t)
		rule := newRule()
		c.deriveRuleStatus(rule, nodeList)
		rule.Status.NodeEvaluations[1].LastEvaluationTime = metav1.NewTime(time.Unix(1, 0))

		released := nodeList.DeepCopy()
		released.Items[0].Spec.Taints = nil
		c.deriveRuleStatus(rule, released)

		g.Expect(rule.Status.NodeEvaluations[1].TaintStatus).To(Equal(readinessv1alpha1.TaintStatusAbsent))
		g.Expect(rule.Status.NodeEvaluations[1].LastEvaluationTime.Time.After(time.Unix(1, 0))).To(BeTrue())
	})

	t.Run("completed bootstrap keeps the last evaluation", func(t *testing.T) {
		g := NewWithT(t)
		rule := newRule()
		rule.UID = "uid-1"
		rule.Spec.EnforcementMode = readinessv1alpha1.EnforcementModeBootstrapOnly
		c.deriveRuleStatus(rule, nodeList)
		before := rule.Status.NodeEvaluations[0].DeepCopy()

		completed := nodeList.DeepCopy()
		completed.Items[1].Annotations = map[string]string{bootstrapAnnotationKey("uid-1"): bootstrapAnnotationValue("r")}
		completed.Items[1].Status.Conditions[0].Status = corev1.ConditionFalse
		c.deriveRuleStatus(rule, completed)

		g.Expect(rule.Status.NodeEvaluations[0]).To(Equal(*before))
	})
}

func TestListRuleInventory(t *testing.T) {
	g := NewWithT(t)

	c := &RuleReadinessController{}
	ctx := t.Context()

	newRule := func(name string, mode readinessv1alpha1.EnforcementMode, dryRun bool, deleting bool) *readinessv1alpha1.NodeReadinessRule {
		rule := &readinessv1alpha1.NodeReadinessRule{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: readinessv1alpha1.NodeReadinessRuleSpec{
				EnforcementMode: mode,
				DryRun:          dryRun,
			},
		}
		if deleting {
			now := metav1.Now()
			rule.DeletionTimestamp = &now
			rule.Finalizers = []string{finalizerName}
		}
		return rule
	}

	rules := []*readinessv1alpha1.NodeReadinessRule{
		newRule("rule-a", readinessv1alpha1.EnforcementModeBootstrapOnly, false, false),
		newRule("rule-b", readinessv1alpha1.EnforcementModeBootstrapOnly, false, false),
		newRule("rule-c", readinessv1alpha1.EnforcementModeContinuous, true, false),
		newRule("rule-deleting", readinessv1alpha1.EnforcementModeContinuous, false, true),
	}

	counts, err := c.ListRuleInventory(ctx, rules)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(counts).To(Equal(map[metrics.RuleModeKey]float64{
		{EnforcementMode: "bootstrap-only", DryRun: false}: 2,
		{EnforcementMode: "continuous", DryRun: true}:      1,
	}))
}
