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
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

func TestListRuleMatchedNodes_NoRules(t *testing.T) {
	g := NewWithT(t)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleMatchedNodes(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(BeEmpty())
}

func TestListRuleMatchedNodes_ZeroMatches(t *testing.T) {
	g := NewWithT(t)
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		gpuRule(),
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-node"}},
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleMatchedNodes(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(Equal(map[string]float64{"gpu-ready": 0}))
}

func TestListRuleMatchedNodes_MixedMatches(t *testing.T) {
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

	counts, err := c.ListRuleMatchedNodes(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(Equal(map[string]float64{"gpu-ready": 3}))
}

func TestListRuleMatchedNodes_DryRunRuleIncluded(t *testing.T) {
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

	counts, err := c.ListRuleMatchedNodes(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(Equal(map[string]float64{"gpu-ready": 1}))
}

func TestListRuleMatchedNodes_DeletingRuleIncluded(t *testing.T) {
	g := NewWithT(t)
	rule := gpuRule()
	now := metav1.Now()
	rule.DeletionTimestamp = &now
	rule.Finalizers = []string{"readiness.node.x-k8s.io/cleanup-taints"}
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		rule,
		gpuNode("held-1", true),
		gpuNode("held-2", true),
	).Build()
	c := &RuleReadinessController{
		Client: fc,
	}

	counts, err := c.ListRuleMatchedNodes(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).To(Equal(map[string]float64{"gpu-ready": 2}))
}

func TestListRuleMatchedNodes_InvalidSelectorSkipped(t *testing.T) {
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

	counts, err := c.ListRuleMatchedNodes(t.Context())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(counts).NotTo(HaveKey(invalidRule.Name))
	g.Expect(counts).To(Equal(map[string]float64{"gpu-ready": 2}))
}
