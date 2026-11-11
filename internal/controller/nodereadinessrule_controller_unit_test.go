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
)

func cond(condType string, status corev1.ConditionStatus) corev1.NodeCondition {
	return corev1.NodeCondition{Type: corev1.NodeConditionType(condType), Status: status}
}

func req(
	condType string,
	required corev1.ConditionStatus,
	defaultStatus ...corev1.ConditionStatus,
) readinessv1alpha1.ConditionRequirement {
	r := readinessv1alpha1.ConditionRequirement{Type: condType, RequiredStatus: required}
	if len(defaultStatus) > 0 {
		r.DefaultStatus = defaultStatus[0]
	}
	return r
}

func workerNode(name string, ready corev1.ConditionStatus) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"node-role": "worker"},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: "Ready", Status: ready},
			},
		},
	}
}

//nolint:unparam // keep name parameter for future multi-node annotation test cases
func workerNodeWithAnnotation(name string, ready corev1.ConditionStatus, key, val string) corev1.Node {
	n := workerNode(name, ready)
	n.Annotations = map[string]string{key: val}
	return n
}

func nonWorkerNode(name string) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"node-role": "control-plane"},
		},
	}
}

func wantSummary(targeted, satisfied, unsatisfied int32) readinessv1alpha1.RuleEvaluationSummary {
	return readinessv1alpha1.RuleEvaluationSummary{
		Targeted:    &targeted,
		Satisfied:   &satisfied,
		Unsatisfied: &unsatisfied,
	}
}

func TestComputeRuleSummary(t *testing.T) {
	testUID := types.UID("rule-uid-1234")

	tests := []struct {
		name   string
		mode   readinessv1alpha1.EnforcementMode
		dryRun bool
		rule   *readinessv1alpha1.NodeReadinessRule // optional override for non-standard rule (e.g. invalid selector)
		nodes  []corev1.Node
		want   readinessv1alpha1.RuleEvaluationSummary
	}{
		{
			name: "mixed satisfied and unsatisfied targeted nodes",
			mode: readinessv1alpha1.EnforcementModeContinuous,
			nodes: []corev1.Node{
				workerNode("w1", corev1.ConditionTrue),
				workerNode("w2", corev1.ConditionFalse),
				workerNode("w3", corev1.ConditionTrue),
			},
			want: wantSummary(3, 2, 1),
		},
		{
			name: "invalid node selector returns empty summary",
			mode: readinessv1alpha1.EnforcementModeContinuous,
			rule: &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid-rule"},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					NodeSelector: metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{Key: "invalid key with spaces", Operator: metav1.LabelSelectorOpIn, Values: []string{"val"}},
						},
					},
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
				},
			},
			nodes: []corev1.Node{
				workerNode("w1", corev1.ConditionTrue),
			},
			want: readinessv1alpha1.RuleEvaluationSummary{},
		},
		{
			name:  "empty node list",
			mode:  readinessv1alpha1.EnforcementModeContinuous,
			nodes: nil,
			want:  wantSummary(0, 0, 0),
		},
		{
			name: "non-matching nodes are excluded from targeted count",
			mode: readinessv1alpha1.EnforcementModeContinuous,
			nodes: []corev1.Node{
				workerNode("worker-1", corev1.ConditionTrue),
				nonWorkerNode("cp-1"),
			},
			want: wantSummary(1, 1, 0),
		},
		{
			name: "bootstrap-only mode with UID annotation is permanently satisfied",
			mode: readinessv1alpha1.EnforcementModeBootstrapOnly,
			nodes: []corev1.Node{
				workerNodeWithAnnotation("w1", corev1.ConditionFalse, bootstrapAnnotationKey(testUID), `{"rule-name":"test-rule"}`),
			},
			want: wantSummary(1, 1, 0),
		},
		{
			name: "bootstrap-only mode with legacy annotation is permanently satisfied",
			mode: readinessv1alpha1.EnforcementModeBootstrapOnly,
			nodes: []corev1.Node{
				workerNodeWithAnnotation("w1", corev1.ConditionFalse, legacyBootstrapAnnotationKey("test-rule"), `{"rule-name":"test-rule"}`),
			},
			want: wantSummary(1, 1, 0),
		},
		{
			name: "bootstrap-only mode without annotation falls back to condition evaluation",
			mode: readinessv1alpha1.EnforcementModeBootstrapOnly,
			nodes: []corev1.Node{
				workerNode("w1", corev1.ConditionFalse),
			},
			want: wantSummary(1, 0, 1),
		},
		{
			name:   "bootstrap-only mode in dryRun ignores bootstrap annotation",
			mode:   readinessv1alpha1.EnforcementModeBootstrapOnly,
			dryRun: true,
			nodes: []corev1.Node{
				workerNodeWithAnnotation("w1", corev1.ConditionFalse, bootstrapAnnotationKey(testUID), `{"rule-name":"test-rule"}`),
			},
			want: wantSummary(1, 0, 1),
		},
		{
			name: "continuous mode ignores bootstrap annotation and evaluates conditions",
			mode: readinessv1alpha1.EnforcementModeContinuous,
			nodes: []corev1.Node{
				workerNodeWithAnnotation("w1", corev1.ConditionFalse, bootstrapAnnotationKey(testUID), `{"rule-name":"test-rule"}`),
			},
			want: wantSummary(1, 0, 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			rule := tt.rule
			if rule == nil {
				rule = &readinessv1alpha1.NodeReadinessRule{
					ObjectMeta: metav1.ObjectMeta{Name: "test-rule", UID: testUID},
					Spec: readinessv1alpha1.NodeReadinessRuleSpec{
						EnforcementMode: tt.mode,
						DryRun:          tt.dryRun,
						NodeSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"node-role": "worker"},
						},
						Conditions: []readinessv1alpha1.ConditionRequirement{
							{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
						},
					},
				}
			}

			nodeList := corev1.NodeList{Items: tt.nodes}
			c := RuleReadinessController{}
			got := c.computeRuleSummary(t.Context(), rule, &nodeList)

			if tt.want.Targeted == nil {
				g.Expect(got.Targeted).To(BeNil())
				g.Expect(got.Satisfied).To(BeNil())
				g.Expect(got.Unsatisfied).To(BeNil())
				return
			}

			g.Expect(got.Targeted).NotTo(BeNil())
			g.Expect(*got.Targeted).To(Equal(*tt.want.Targeted))

			g.Expect(got.Satisfied).NotTo(BeNil())
			g.Expect(*got.Satisfied).To(Equal(*tt.want.Satisfied))

			g.Expect(got.Unsatisfied).NotTo(BeNil())
			g.Expect(*got.Unsatisfied).To(Equal(*tt.want.Unsatisfied))

			// Verify invariant: targeted == satisfied + unsatisfied
			g.Expect(*got.Targeted).To(Equal(*got.Satisfied + *got.Unsatisfied))
		})
	}
}

func TestNodeSatisfiesRule(t *testing.T) {
	tests := []struct {
		name   string
		policy readinessv1alpha1.ConditionPolicy
		reqs   []readinessv1alpha1.ConditionRequirement
		conds  []corev1.NodeCondition
		want   bool
	}{
		// allOf policy
		{
			name:   "allOf: all conditions satisfied",
			policy: readinessv1alpha1.ConditionPolicyAllOf,
			reqs:   []readinessv1alpha1.ConditionRequirement{req("GPU", corev1.ConditionTrue), req("CNI", corev1.ConditionFalse)},
			conds:  []corev1.NodeCondition{cond("GPU", corev1.ConditionTrue), cond("CNI", corev1.ConditionFalse)},
			want:   true,
		},
		{
			name:   "allOf: one condition unsatisfied",
			policy: readinessv1alpha1.ConditionPolicyAllOf,
			reqs:   []readinessv1alpha1.ConditionRequirement{req("GPU", corev1.ConditionTrue), req("CNI", corev1.ConditionTrue)},
			conds:  []corev1.NodeCondition{cond("GPU", corev1.ConditionTrue), cond("CNI", corev1.ConditionFalse)},
			want:   false,
		},
		{
			name:   "allOf: missing condition satisfied by matching defaultStatus",
			policy: readinessv1alpha1.ConditionPolicyAllOf,
			reqs:   []readinessv1alpha1.ConditionRequirement{req("GPU", corev1.ConditionTrue), req("CNI", corev1.ConditionTrue, corev1.ConditionTrue)},
			conds:  []corev1.NodeCondition{cond("GPU", corev1.ConditionTrue)},
			want:   true,
		},
		{
			name:   "allOf: default policy (empty string) behaves as allOf",
			policy: "",
			reqs:   []readinessv1alpha1.ConditionRequirement{req("GPU", corev1.ConditionTrue)},
			conds:  []corev1.NodeCondition{cond("GPU", corev1.ConditionTrue)},
			want:   true,
		},

		// anyOf policy
		{
			name:   "anyOf: all conditions satisfied",
			policy: readinessv1alpha1.ConditionPolicyAnyOf,
			reqs:   []readinessv1alpha1.ConditionRequirement{req("GPU", corev1.ConditionTrue), req("CNI", corev1.ConditionTrue)},
			conds:  []corev1.NodeCondition{cond("GPU", corev1.ConditionTrue), cond("CNI", corev1.ConditionTrue)},
			want:   true,
		},
		{
			name:   "anyOf: one satisfied and one unsatisfied",
			policy: readinessv1alpha1.ConditionPolicyAnyOf,
			reqs:   []readinessv1alpha1.ConditionRequirement{req("GPU", corev1.ConditionTrue), req("CNI", corev1.ConditionTrue)},
			conds:  []corev1.NodeCondition{cond("GPU", corev1.ConditionTrue), cond("CNI", corev1.ConditionFalse)},
			want:   true,
		},
		{
			name:   "anyOf: none satisfied",
			policy: readinessv1alpha1.ConditionPolicyAnyOf,
			reqs:   []readinessv1alpha1.ConditionRequirement{req("GPU", corev1.ConditionTrue), req("CNI", corev1.ConditionTrue)},
			conds:  []corev1.NodeCondition{cond("GPU", corev1.ConditionFalse), cond("CNI", corev1.ConditionFalse)},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			c := RuleReadinessController{}
			rule := readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-rule"},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					ConditionPolicy: tt.policy,
					Conditions:      tt.reqs,
				},
			}
			node := corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
				Status:     corev1.NodeStatus{Conditions: tt.conds},
			}
			g.Expect(c.nodeSatisfiesRule(&node, &rule)).To(Equal(tt.want))
		})
	}
}
