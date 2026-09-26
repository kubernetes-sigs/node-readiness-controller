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

func TestGetApplicableRulesForNode_DeepCopy(t *testing.T) {
	g := NewWithT(t)

	c := &RuleReadinessController{
		ruleCache: make(map[string]*readinessv1alpha1.NodeReadinessRule),
	}

	rule := &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule-1"},
		Spec: readinessv1alpha1.NodeReadinessRuleSpec{
			NodeSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "prod"},
			},
		},
		Status: readinessv1alpha1.NodeReadinessRuleStatus{
			AppliedNodes: []string{"node-1"},
		},
	}

	ctx := t.Context()
	c.updateRuleCache(ctx, rule)

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node-1",
			Labels: map[string]string{"env": "prod"},
		},
	}

	rules := c.getApplicableRulesForNode(ctx, node)
	g.Expect(rules).To(HaveLen(1))

	// Mutate the returned rule's status
	rules[0].Status.AppliedNodes = append(rules[0].Status.AppliedNodes, "node-2")

	// Ensure the cached rule was isolated and not mutated
	c.ruleCacheMutex.RLock()
	cachedRule := c.ruleCache["rule-1"]
	c.ruleCacheMutex.RUnlock()

	g.Expect(cachedRule.Status.AppliedNodes).To(Equal([]string{"node-1"}))
}

func TestComputeSummary(t *testing.T) {
	g := NewWithT(t)

	rule := &readinessv1alpha1.NodeReadinessRule{
		Status: readinessv1alpha1.NodeReadinessRuleStatus{
			NodeEvaluations: []readinessv1alpha1.NodeEvaluation{
				{NodeName: "node-1", TaintStatus: readinessv1alpha1.TaintStatusPresent},
				{NodeName: "node-2", TaintStatus: readinessv1alpha1.TaintStatusAbsent},
				{NodeName: "node-3", TaintStatus: readinessv1alpha1.TaintStatusPresent},
			},
			FailedNodes: []readinessv1alpha1.NodeFailure{
				{NodeName: "node-4"},
			},
		},
	}

	computeSummary(rule)

	g.Expect(rule.Status.Summary).NotTo(BeNil())
	g.Expect(rule.Status.Summary.MatchedNodes).To(Equal(int32(4)))
	g.Expect(rule.Status.Summary.HeldNodes).To(Equal(int32(2)))
	g.Expect(rule.Status.Summary.ReleasedNodes).To(Equal(int32(1)))
	g.Expect(rule.Status.Summary.FailedNodes).To(Equal(int32(1)))
}

func TestApplyNodeStatusDelta(t *testing.T) {
	g := NewWithT(t)

	t.Run("empty delta leaves status untouched", func(t *testing.T) {
		rule := &readinessv1alpha1.NodeReadinessRule{
			Status: readinessv1alpha1.NodeReadinessRuleStatus{
				NodeEvaluations: []readinessv1alpha1.NodeEvaluation{
					{NodeName: "node-1"},
				},
				FailedNodes: []readinessv1alpha1.NodeFailure{
					{NodeName: "node-1", Reason: "Err"},
				},
			},
		}

		delta := nodeStatusDelta{
			evaluations: nil,
			failures:    nil,
		}

		applyNodeStatusDelta(rule, delta)
		g.Expect(rule.Status.NodeEvaluations).To(HaveLen(1))
		g.Expect(rule.Status.NodeEvaluations[0].NodeName).To(Equal("node-1"))
		g.Expect(rule.Status.FailedNodes).To(HaveLen(1))
		g.Expect(rule.Status.FailedNodes[0].NodeName).To(Equal("node-1"))
		g.Expect(rule.Status.Summary).NotTo(BeNil())
		g.Expect(rule.Status.Summary.MatchedNodes).To(Equal(int32(2)))
	})

	t.Run("merges evaluation updates and new evaluations in sorted order", func(t *testing.T) {
		rule := &readinessv1alpha1.NodeReadinessRule{
			Status: readinessv1alpha1.NodeReadinessRuleStatus{
				NodeEvaluations: []readinessv1alpha1.NodeEvaluation{
					{NodeName: "node-1", TaintStatus: readinessv1alpha1.TaintStatusAbsent},
					{NodeName: "node-3", TaintStatus: readinessv1alpha1.TaintStatusAbsent},
				},
			},
		}

		delta := nodeStatusDelta{
			evaluations: map[string]readinessv1alpha1.NodeEvaluation{
				"node-1": {NodeName: "node-1", TaintStatus: readinessv1alpha1.TaintStatusPresent}, // update existing
				"node-2": {NodeName: "node-2", TaintStatus: readinessv1alpha1.TaintStatusPresent}, // add new
			},
		}

		applyNodeStatusDelta(rule, delta)

		g.Expect(rule.Status.NodeEvaluations).To(HaveLen(3))
		g.Expect(rule.Status.NodeEvaluations[0].NodeName).To(Equal("node-1"))
		g.Expect(rule.Status.NodeEvaluations[0].TaintStatus).To(Equal(readinessv1alpha1.TaintStatusPresent))
		g.Expect(rule.Status.NodeEvaluations[1].NodeName).To(Equal("node-2"))
		g.Expect(rule.Status.NodeEvaluations[2].NodeName).To(Equal("node-3"))
		g.Expect(rule.Status.NodeEvaluations[2].TaintStatus).To(Equal(readinessv1alpha1.TaintStatusAbsent))
		g.Expect(rule.Status.Summary).NotTo(BeNil())
		g.Expect(rule.Status.Summary.HeldNodes).To(Equal(int32(2)))
		g.Expect(rule.Status.Summary.ReleasedNodes).To(Equal(int32(1)))
	})

	t.Run("merges failures and clears failure when nil in delta", func(t *testing.T) {
		rule := &readinessv1alpha1.NodeReadinessRule{
			Status: readinessv1alpha1.NodeReadinessRuleStatus{
				FailedNodes: []readinessv1alpha1.NodeFailure{
					{NodeName: "node-1", Reason: "OldError"},
					{NodeName: "node-3", Reason: "PersistentError"},
				},
			},
		}

		delta := nodeStatusDelta{
			failures: map[string]*readinessv1alpha1.NodeFailure{
				"node-1": nil,                                      // clear failure for node-1
				"node-2": {NodeName: "node-2", Reason: "NewError"}, // add failure for node-2
			},
		}

		applyNodeStatusDelta(rule, delta)

		g.Expect(rule.Status.FailedNodes).To(HaveLen(2))
		g.Expect(rule.Status.FailedNodes[0].NodeName).To(Equal("node-2"))
		g.Expect(rule.Status.FailedNodes[0].Reason).To(Equal("NewError"))
		g.Expect(rule.Status.FailedNodes[1].NodeName).To(Equal("node-3"))
		g.Expect(rule.Status.FailedNodes[1].Reason).To(Equal("PersistentError"))
	})

	t.Run("does not add zero-value evaluation when evaluations map has no entry for node", func(t *testing.T) {
		rule := &readinessv1alpha1.NodeReadinessRule{
			Status: readinessv1alpha1.NodeReadinessRuleStatus{
				NodeEvaluations: []readinessv1alpha1.NodeEvaluation{
					{NodeName: "other-node", TaintStatus: readinessv1alpha1.TaintStatusPresent},
				},
			},
		}

		delta := nodeStatusDelta{
			evaluations: make(map[string]readinessv1alpha1.NodeEvaluation),
			failures: map[string]*readinessv1alpha1.NodeFailure{
				"probe-node": {NodeName: "probe-node", Reason: "EvaluationError"},
			},
		}

		applyNodeStatusDelta(rule, delta)

		g.Expect(rule.Status.NodeEvaluations).To(HaveLen(1))
		g.Expect(rule.Status.NodeEvaluations[0].NodeName).To(Equal("other-node"))
		g.Expect(rule.Status.FailedNodes).To(HaveLen(1))
		g.Expect(rule.Status.FailedNodes[0].NodeName).To(Equal("probe-node"))
	})

	t.Run("clears stale failure when evaluation succeeds and produces new evaluation", func(t *testing.T) {
		rule := &readinessv1alpha1.NodeReadinessRule{
			Status: readinessv1alpha1.NodeReadinessRuleStatus{
				NodeEvaluations: []readinessv1alpha1.NodeEvaluation{
					{NodeName: "other-node", TaintStatus: readinessv1alpha1.TaintStatusPresent},
				},
				FailedNodes: []readinessv1alpha1.NodeFailure{
					{NodeName: "node-1", Reason: "EvaluationError", Message: "old failure"},
				},
			},
		}

		delta := nodeStatusDelta{
			evaluations: map[string]readinessv1alpha1.NodeEvaluation{
				"node-1": {NodeName: "node-1", TaintStatus: readinessv1alpha1.TaintStatusAbsent},
			},
			failures: map[string]*readinessv1alpha1.NodeFailure{
				"node-1": nil, // clear failure for node-1 on success
			},
		}

		applyNodeStatusDelta(rule, delta)

		g.Expect(rule.Status.NodeEvaluations).To(HaveLen(2))
		g.Expect(rule.Status.NodeEvaluations[0].NodeName).To(Equal("node-1"))
		g.Expect(rule.Status.NodeEvaluations[0].TaintStatus).To(Equal(readinessv1alpha1.TaintStatusAbsent))
		g.Expect(rule.Status.NodeEvaluations[1].NodeName).To(Equal("other-node"))
		g.Expect(rule.Status.FailedNodes).To(BeEmpty())
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
