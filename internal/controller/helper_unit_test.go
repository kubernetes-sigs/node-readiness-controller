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
