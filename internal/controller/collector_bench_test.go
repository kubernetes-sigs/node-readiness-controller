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
func buildBenchController(b *testing.B, nodeCount, ruleCount int) (*RuleReadinessController, []corev1.Node) {
	b.Helper()

	scheme := newTestScheme(b)
	objs := make([]client.Object, 0, ruleCount)

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

	nodes := make([]corev1.Node, nodeCount)
	for i := range nodeCount {
		ruleIdx := i % ruleCount
		ruleName := fmt.Sprintf("rule-%d", ruleIdx)
		nodes[i] = corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   fmt.Sprintf("node-%d", i),
				Labels: map[string]string{"rule-index": fmt.Sprintf("%d", ruleIdx)},
			},
		}
		if i%3 == 0 {
			nodes[i].Spec.Taints = []corev1.Taint{rules[ruleName].Spec.Taint}
		}
	}

	fc := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	return &RuleReadinessController{Client: fc}, nodes
}

func BenchmarkListRuleNodeStates(b *testing.B) {
	nodeCounts := []int{100, 1000, 5000, 15000}
	ruleCounts := []int{5, 20, 50}

	for _, nodeCount := range nodeCounts {
		for _, ruleCount := range ruleCounts {
			b.Run(fmt.Sprintf("nodes=%d/rules=%d", nodeCount, ruleCount), func(b *testing.B) {
				c, nodes := buildBenchController(b, nodeCount, ruleCount)
				ctx := b.Context()

				b.ResetTimer()
				b.ReportAllocs()
				for range b.N {
					if _, err := c.ListRuleNodeStates(ctx, nodes); err != nil {
						b.Fatalf("ListRuleNodeStates failed: %v", err)
					}
				}
			})
		}
	}
}

// buildBlockedNodesBenchController sets up rules and evaluations for benchmarking ListBlockedNodes.
func buildBlockedNodesBenchController(b *testing.B, nodeCount, ruleCount, conditionsPerRule int) (*RuleReadinessController, []corev1.Node) {
	b.Helper()

	scheme := newTestScheme(b)
	objs := make([]client.Object, 0, ruleCount)

	rules := make([]*readinessv1alpha1.NodeReadinessRule, 0, ruleCount)
	for i := range ruleCount {
		ruleName := fmt.Sprintf("rule-%d", i)
		conditions := make([]readinessv1alpha1.ConditionRequirement, 0, conditionsPerRule)
		for j := range conditionsPerRule {
			conditions = append(conditions, readinessv1alpha1.ConditionRequirement{
				Type:           fmt.Sprintf("Condition-%d", j),
				RequiredStatus: corev1.ConditionTrue,
			})
		}
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
				Conditions: conditions,
			},
		}
		rules = append(rules, rule)
		objs = append(objs, rule)
	}

	nodes := make([]corev1.Node, nodeCount)
	for i := range nodeCount {
		ruleIdx := i % ruleCount
		rule := rules[ruleIdx]

		nodeConditions := make([]corev1.NodeCondition, 0, conditionsPerRule)
		for k, cond := range rule.Spec.Conditions {
			status := corev1.ConditionTrue
			if k%2 == 0 {
				status = corev1.ConditionFalse
			}
			nodeConditions = append(nodeConditions, corev1.NodeCondition{
				Type:   corev1.NodeConditionType(cond.Type),
				Status: status,
			})
		}

		nodes[i] = corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   fmt.Sprintf("node-%d", i),
				Labels: map[string]string{"rule-index": fmt.Sprintf("%d", ruleIdx)},
			},
			Status: corev1.NodeStatus{Conditions: nodeConditions},
		}
		if i%3 == 0 {
			nodes[i].Spec.Taints = []corev1.Taint{rule.Spec.Taint}
		}
	}

	fc := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	return &RuleReadinessController{Client: fc}, nodes
}

// BenchmarkListBlockedNodes measures the performance of ListBlockedNodes.
func BenchmarkListBlockedNodes(b *testing.B) {
	nodeCounts := []int{100, 1000, 5000, 15000}
	ruleCounts := []int{5, 20, 50}
	conditionsPerRuleOptions := []int{1, 4, 8}

	for _, nodeCount := range nodeCounts {
		for _, ruleCount := range ruleCounts {
			for _, conditionsPerRule := range conditionsPerRuleOptions {
				b.Run(fmt.Sprintf("nodes=%d/rules=%d/conditions=%d", nodeCount, ruleCount, conditionsPerRule), func(b *testing.B) {
					c, nodes := buildBlockedNodesBenchController(b, nodeCount, ruleCount, conditionsPerRule)
					ctx := b.Context()

					b.ResetTimer()
					b.ReportAllocs()
					for range b.N {
						if _, err := c.ListBlockedNodes(ctx, nodes); err != nil {
							b.Fatalf("ListBlockedNodes failed: %v", err)
						}
					}
				})
			}
		}
	}
}

// buildFullBenchController sets up rules and nodes for benchmarking the full collector path.
func buildFullBenchController(b *testing.B, nodeCount, ruleCount, conditionsPerRule int) *RuleReadinessController {
	b.Helper()

	scheme := newTestScheme(b)
	objs := make([]client.Object, 0, nodeCount+ruleCount)

	rules := make([]*readinessv1alpha1.NodeReadinessRule, 0, ruleCount)
	for i := range ruleCount {
		ruleName := fmt.Sprintf("rule-%d", i)
		conditions := make([]readinessv1alpha1.ConditionRequirement, 0, conditionsPerRule)
		for j := range conditionsPerRule {
			conditions = append(conditions, readinessv1alpha1.ConditionRequirement{
				Type:           fmt.Sprintf("Condition-%d", j),
				RequiredStatus: corev1.ConditionTrue,
			})
		}
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
				Conditions: conditions,
			},
		}
		rules = append(rules, rule)
	}

	nodes := make([]corev1.Node, nodeCount)
	for i := range nodeCount {
		ruleIdx := i % ruleCount
		rule := rules[ruleIdx]
		nodeName := fmt.Sprintf("node-%d", i)

		nodeConditions := make([]corev1.NodeCondition, 0, conditionsPerRule)
		for k, cond := range rule.Spec.Conditions {
			status := corev1.ConditionTrue
			if k%2 == 0 {
				status = corev1.ConditionFalse
			}
			nodeConditions = append(nodeConditions, corev1.NodeCondition{
				Type:   corev1.NodeConditionType(cond.Type),
				Status: status,
			})
		}

		nodes[i] = corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nodeName,
				Labels: map[string]string{"rule-index": fmt.Sprintf("%d", ruleIdx)},
			},
			Status: corev1.NodeStatus{Conditions: nodeConditions},
		}
		if i%3 == 0 {
			nodes[i].Spec.Taints = []corev1.Taint{rule.Spec.Taint}
		}
	}

	for _, rule := range rules {
		objs = append(objs, rule)
	}
	for i := range nodes {
		objs = append(objs, &nodes[i])
	}

	fc := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	return &RuleReadinessController{Client: fc}
}

// BenchmarkCollectSharedNodeList measures the collector path with a shared Node list for ListRuleNodeStates and ListBlockedNodes.
func BenchmarkCollectSharedNodeList(b *testing.B) {
	nodeCounts := []int{100, 1000, 5000, 15000}
	ruleCounts := []int{5, 20, 50}
	conditionsPerRuleOptions := []int{1, 4, 8}

	for _, nodeCount := range nodeCounts {
		for _, ruleCount := range ruleCounts {
			for _, conditionsPerRule := range conditionsPerRuleOptions {
				b.Run(fmt.Sprintf("nodes=%d/rules=%d/conditions=%d", nodeCount, ruleCount, conditionsPerRule), func(b *testing.B) {
					c := buildFullBenchController(b, nodeCount, ruleCount, conditionsPerRule)
					ctx := b.Context()

					b.ResetTimer()
					b.ReportAllocs()
					for range b.N {
						nodes, err := c.ListNodes(ctx)
						if err != nil {
							b.Fatalf("ListNodes failed: %v", err)
						}
						if _, err := c.ListRuleNodeStates(ctx, nodes); err != nil {
							b.Fatalf("ListRuleNodeStates failed: %v", err)
						}
						if _, err := c.ListBlockedNodes(ctx, nodes); err != nil {
							b.Fatalf("ListBlockedNodes failed: %v", err)
						}
					}
				})
			}
		}
	}
}
