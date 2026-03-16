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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

var _ = Describe("NodeReadinessRuleReport Controller", func() {
	var (
		ctx        context.Context
		reconciler *NodeReadinessRuleReportReconciler
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Clean up Nodes
		var nodeList corev1.NodeList
		Expect(k8sClient.List(ctx, &nodeList)).To(Succeed())
		for _, obj := range nodeList.Items {
			if len(obj.Finalizers) > 0 {
				obj.Finalizers = nil
				_ = k8sClient.Update(ctx, &obj)
			}
			_ = k8sClient.Delete(ctx, &obj)
		}

		// Clean up Rules
		var ruleList readinessv1alpha1.NodeReadinessRuleList
		Expect(k8sClient.List(ctx, &ruleList)).To(Succeed())
		for _, obj := range ruleList.Items {
			if len(obj.Finalizers) > 0 {
				obj.Finalizers = nil
				_ = k8sClient.Update(ctx, &obj)
			}
			_ = k8sClient.Delete(ctx, &obj)
		}

		// Clean up Reports
		var reportList readinessv1alpha1.NodeReadinessRuleReportList
		Expect(k8sClient.List(ctx, &reportList)).To(Succeed())
		for _, obj := range reportList.Items {
			_ = k8sClient.Delete(ctx, &obj)
		}

		// Wait for the API server to actually finish purging the objects
		Eventually(func() bool {
			var currentRules readinessv1alpha1.NodeReadinessRuleList
			var currentNodes corev1.NodeList
			var currentReports readinessv1alpha1.NodeReadinessRuleReportList

			_ = k8sClient.List(ctx, &currentRules)
			_ = k8sClient.List(ctx, &currentNodes)
			_ = k8sClient.List(ctx, &currentReports)

			return len(currentRules.Items) == 0 &&
				len(currentNodes.Items) == 0 &&
				len(currentReports.Items) == 0
		}, time.Second*5, time.Millisecond*100).Should(BeTrue(), "Failed to clean up prior test resources")

		// Initialize the reconciler for the fresh, clean test
		reconciler = &NodeReadinessRuleReportReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	})

	AfterEach(func() {
		// 1. Clean up all NodeReadinessRuleReports
		var reportList readinessv1alpha1.NodeReadinessRuleReportList
		Expect(k8sClient.List(ctx, &reportList)).To(Succeed())
		for _, report := range reportList.Items {
			_ = k8sClient.Delete(ctx, &report)
		}

		// 2. Clean up all NodeReadinessRules
		var ruleList readinessv1alpha1.NodeReadinessRuleList
		Expect(k8sClient.List(ctx, &ruleList)).To(Succeed())
		for _, rule := range ruleList.Items {
			if len(rule.Finalizers) > 0 {
				rule.Finalizers = nil
				_ = k8sClient.Update(ctx, &rule)
			}
			_ = k8sClient.Delete(ctx, &rule)
		}

		// 3. Clean up all Nodes
		var nodeList corev1.NodeList
		Expect(k8sClient.List(ctx, &nodeList)).To(Succeed())
		for _, node := range nodeList.Items {
			if len(node.Finalizers) > 0 {
				node.Finalizers = nil
				_ = k8sClient.Update(ctx, &node)
			}
			_ = k8sClient.Delete(ctx, &node)
		}
	})

	Context("When reconciling a Node", func() {
		It("Should create a Report with no matched rules if no rules exist", func() {
			nodeName := "test-node-empty"
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName,
				},
			}
			Expect(k8sClient.Create(ctx, node)).Should(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: nodeName}}
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			reportLookupKey := types.NamespacedName{Name: getNodeReadinessRuleReportName(nodeName)}
			createdReport := &readinessv1alpha1.NodeReadinessRuleReport{}

			Eventually(func() error {
				return k8sClient.Get(ctx, reportLookupKey, createdReport)
			}, time.Second*2, time.Millisecond*50).Should(Succeed())

			Expect(createdReport.Spec.NodeName).Should(Equal(nodeName))
			Expect(createdReport.Status.ReadinessReports).Should(BeEmpty())
			Expect(*createdReport.Status.Summary.MatchedRules).Should(Equal(int32(0)))
		})

		It("Should accurately evaluate a Node against a matching Rule", func() {
			ruleName := "test-rule-match"
			nodeName := "test-node-match"

			rule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: ruleName,
				},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "production"},
					},
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "dedicated",
						Value:  "true",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeContinuous,
				},
			}
			Expect(k8sClient.Create(ctx, rule)).Should(Succeed())

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nodeName,
					Labels: map[string]string{"env": "production"},
				},
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{Key: "dedicated", Value: "true", Effect: corev1.TaintEffectNoSchedule},
					},
				},
			}
			Expect(k8sClient.Create(ctx, node)).Should(Succeed())

			node.Status = corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue},
				},
			}
			Expect(k8sClient.Status().Update(ctx, node)).Should(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: nodeName}}
			Eventually(func() error {
				_, err := reconciler.Reconcile(ctx, req)
				if err != nil {
					return err
				}

				reportLookupKey := types.NamespacedName{Name: getNodeReadinessRuleReportName(nodeName)}
				createdReport := &readinessv1alpha1.NodeReadinessRuleReport{}
				if err := k8sClient.Get(ctx, reportLookupKey, createdReport); err != nil {
					return err
				}
				if len(createdReport.Status.ReadinessReports) == 0 {
					return fmt.Errorf("cache not synced yet")
				}
				return nil
			}, time.Second*5, time.Millisecond*100).Should(Succeed())

			// Immediate Assertions
			reportLookupKey := types.NamespacedName{Name: getNodeReadinessRuleReportName(nodeName)}
			createdReport := &readinessv1alpha1.NodeReadinessRuleReport{}
			Expect(k8sClient.Get(ctx, reportLookupKey, createdReport)).Should(Succeed())

			Expect(*createdReport.Status.Summary.MatchedRules).Should(Equal(int32(1)))
			Expect(*createdReport.Status.Summary.AppliedTaints).Should(Equal(int32(1)))

			report := createdReport.Status.ReadinessReports[0]
			Expect(report.RuleName).Should(Equal(ruleName))
			Expect(report.RuleStatus).Should(Equal(readinessv1alpha1.RuleStatusMatched))
			Expect(report.TaintStatus).Should(Equal(readinessv1alpha1.TaintStatusPresent))
			Expect(report.Reason).Should(Equal("CriteriaMet"))
		})

		It("Should mark a rule as Unmatched if NodeSelector fails", func() {
			ruleName := "test-rule-mismatch-label"
			nodeName := "test-node-mismatch-label"

			rule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: ruleName},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "production"},
					},
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "dedicated",
						Value:  "true",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeContinuous,
				},
			}
			Expect(k8sClient.Create(ctx, rule)).Should(Succeed())

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nodeName,
					Labels: map[string]string{"env": "staging"},
				},
			}
			Expect(k8sClient.Create(ctx, node)).Should(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: nodeName}}
			Eventually(func() error {
				_, err := reconciler.Reconcile(ctx, req)
				return err
			}, time.Second*2, time.Millisecond*100).Should(Succeed())

			reportLookupKey := types.NamespacedName{Name: getNodeReadinessRuleReportName(nodeName)}
			createdReport := &readinessv1alpha1.NodeReadinessRuleReport{}

			Eventually(func() string {
				_ = k8sClient.Get(ctx, reportLookupKey, createdReport)
				if len(createdReport.Status.ReadinessReports) > 0 {
					return createdReport.Status.ReadinessReports[0].Reason
				}
				return ""
			}, time.Second*2, time.Millisecond*100).Should(Equal("SelectorMismatch"))
		})

		It("Should mark a rule as Unmatched if a required condition status does not match", func() {
			ruleName := "rule-condition-mismatch"
			nodeName := "node-condition-mismatch"

			rule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: ruleName},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "staging"},
					},
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "dedicated",
						Value:  "true",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeContinuous,
				},
			}
			Expect(k8sClient.Create(ctx, rule)).Should(Succeed())

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nodeName,
					Labels: map[string]string{"env": "staging"},
				},
			}
			Expect(k8sClient.Create(ctx, node)).Should(Succeed())

			node.Status = corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionFalse},
				},
			}
			Expect(k8sClient.Status().Update(ctx, node)).Should(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: nodeName}}
			Eventually(func() error {
				_, err := reconciler.Reconcile(ctx, req)
				return err
			}, time.Second*2, time.Millisecond*100).Should(Succeed())

			reportLookupKey := types.NamespacedName{Name: getNodeReadinessRuleReportName(nodeName)}
			createdReport := &readinessv1alpha1.NodeReadinessRuleReport{}

			Eventually(func() string {
				_ = k8sClient.Get(ctx, reportLookupKey, createdReport)
				if len(createdReport.Status.ReadinessReports) > 0 {
					return createdReport.Status.ReadinessReports[0].Reason
				}
				return ""
			}, time.Second*2, time.Millisecond*100).Should(Equal("ConditionStatusMismatch"))
		})

		It("Should mark a rule as Unmatched if a required condition is completely missing", func() {
			ruleName := "rule-condition-missing"
			nodeName := "node-condition-missing"

			rule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: ruleName},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "staging"},
					},
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "CustomReady", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "dedicated",
						Value:  "true",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeContinuous,
				},
			}
			Expect(k8sClient.Create(ctx, rule)).Should(Succeed())

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nodeName,
					Labels: map[string]string{"env": "staging"},
				},
			}
			Expect(k8sClient.Create(ctx, node)).Should(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: nodeName}}
			Eventually(func() error {
				_, err := reconciler.Reconcile(ctx, req)
				return err
			}, time.Second*2, time.Millisecond*100).Should(Succeed())

			reportLookupKey := types.NamespacedName{Name: getNodeReadinessRuleReportName(nodeName)}
			createdReport := &readinessv1alpha1.NodeReadinessRuleReport{}

			Eventually(func() string {
				_ = k8sClient.Get(ctx, reportLookupKey, createdReport)
				if len(createdReport.Status.ReadinessReports) > 0 {
					return createdReport.Status.ReadinessReports[0].Reason
				}
				return ""
			}, time.Second*2, time.Millisecond*100).Should(Equal("ConditionNotFound"))
		})

		It("Should identify when a Node matches the rule but is missing the expected Taint", func() {
			ruleName := "rule-taint-absent"
			nodeName := "node-taint-absent"

			rule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: ruleName},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "production"},
					},
					Taint: corev1.Taint{
						Key: "special-hardware", Value: "true", Effect: corev1.TaintEffectNoSchedule,
					},
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeContinuous,
				},
			}
			Expect(k8sClient.Create(ctx, rule)).Should(Succeed())

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nodeName,
					Labels: map[string]string{"env": "staging"},
				},
			}
			Expect(k8sClient.Create(ctx, node)).Should(Succeed())

			node.Status = corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue},
				},
			}
			Expect(k8sClient.Status().Update(ctx, node)).Should(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: nodeName}}
			Eventually(func() error {
				_, err := reconciler.Reconcile(ctx, req)
				return err
			}, time.Second*2, time.Millisecond*100).Should(Succeed())

			reportLookupKey := types.NamespacedName{Name: getNodeReadinessRuleReportName(nodeName)}
			createdReport := &readinessv1alpha1.NodeReadinessRuleReport{}

			Eventually(func() readinessv1alpha1.TaintStatus {
				_ = k8sClient.Get(ctx, reportLookupKey, createdReport)
				if len(createdReport.Status.ReadinessReports) > 0 {
					return createdReport.Status.ReadinessReports[0].TaintStatus
				}
				return ""
			}, time.Second*2, time.Millisecond*100).Should(Equal(readinessv1alpha1.TaintStatusAbsent))
		})

		It("Should generate an error report if the Rule has an invalid NodeSelector", func() {
			ruleName := "rule-invalid-selector"
			nodeName := "node-invalid-selector"

			rule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: ruleName},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					NodeSelector: metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "env",
								Operator: "InvalidOperator", // This will cause parsing to fail
								Values:   []string{"prod"},
							},
						},
					},
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "dedicated",
						Value:  "true",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeContinuous,
				},
			}
			Expect(k8sClient.Create(ctx, rule)).Should(Succeed())

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nodeName,
					Labels: map[string]string{"env": "staging"},
				},
			}
			Expect(k8sClient.Create(ctx, node)).Should(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: nodeName}}
			Eventually(func() error {
				_, err := reconciler.Reconcile(ctx, req)
				return err
			}, time.Second*2, time.Millisecond*100).Should(Succeed())

			reportLookupKey := types.NamespacedName{Name: getNodeReadinessRuleReportName(nodeName)}
			createdReport := &readinessv1alpha1.NodeReadinessRuleReport{}

			Eventually(func() readinessv1alpha1.RuleStatus {
				_ = k8sClient.Get(ctx, reportLookupKey, createdReport)
				if len(createdReport.Status.ReadinessReports) > 0 {
					return createdReport.Status.ReadinessReports[0].RuleStatus
				}
				return ""
			}, time.Second*2, time.Millisecond*100).Should(Equal(readinessv1alpha1.RuleStatusError))

			// Verify the error was tallied in the summary
			Expect(*createdReport.Status.Summary.Errors).Should(Equal(int32(1)))
		})
	})
})
