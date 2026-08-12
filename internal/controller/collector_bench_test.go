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
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

// buildBenchController creates a fake client with the given nodes and rules for benchmarking.
func buildBenchController(b *testing.B, nodeCount, ruleCount int) *RuleReadinessController {
	b.Helper()

	scheme := newTestScheme(b)
	nodes := make([]client.Object, 0, nodeCount)
	objs := make([]client.Object, 0, nodeCount+ruleCount)

	rules := make(map[string]*readinessv1alpha1.NodeReadinessRule, ruleCount)
	for i := range ruleCount {
		ruleName := fmt.Sprintf("rule-%d", i)
		rule := &readinessv1alpha1.NodeReadinessRule{
			ObjectMeta: metav1.ObjectMeta{Name: ruleName},
			Spec: readinessv1alpha1.NodeReadinessRuleSpec{
				NodeSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"rule-index": fmt.Sprintf("%d", i)},
				},
				Taint: corev1.Taint{
					Key:    fmt.Sprintf("readiness.k8s.io/%s", ruleName),
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		}
		rules[ruleName] = rule
		objs = append(objs, rule)
	}

	for i := range nodeCount {
		ruleIdx := i % ruleCount
		ruleName := fmt.Sprintf("rule-%d", ruleIdx)
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   fmt.Sprintf("node-%d", i),
				Labels: map[string]string{"rule-index": fmt.Sprintf("%d", ruleIdx)},
			},
		}
		if i%3 == 0 {
			node.Spec.Taints = []corev1.Taint{rules[ruleName].Spec.Taint}
		}
		nodes = append(nodes, node)
	}
	objs = append(objs, nodes...)

	fc := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	return &RuleReadinessController{
		Client: fc,
	}
}

func BenchmarkListRuleNodeStates(b *testing.B) {
	nodeCounts := []int{100, 1000, 5000, 15000}
	ruleCounts := []int{5, 20, 50}

	for _, nodeCount := range nodeCounts {
		for _, ruleCount := range ruleCounts {
			b.Run(fmt.Sprintf("nodes=%d/rules=%d", nodeCount, ruleCount), func(b *testing.B) {
				c := buildBenchController(b, nodeCount, ruleCount)
				ctx := b.Context()

				b.ResetTimer()
				b.ReportAllocs()
				for range b.N {
					if _, err := c.ListRuleNodeStates(ctx); err != nil {
						b.Fatalf("ListRuleNodeStates failed: %v", err)
					}
				}
			})
		}
	}
}
