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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nodereadinessiov1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

var _ = Describe("Node Controller", func() {
	const (
		nodeName      = "node-controller-test-node"
		ruleName      = "node-controller-test-rule"
		taintKey      = "test-taint"
		conditionType = "TestCondition"
	)

	// pure function tests
	Context("Helper function tests", func() {
		It("should correctly compare node conditions", func() {
			cond1 := []corev1.NodeCondition{
				{Type: "Ready", Status: corev1.ConditionTrue},
				{Type: "NetworkReady", Status: corev1.ConditionFalse},
			}
			cond2 := []corev1.NodeCondition{
				{Type: "Ready", Status: corev1.ConditionTrue},
				{Type: "NetworkReady", Status: corev1.ConditionFalse},
			}
			cond3 := []corev1.NodeCondition{
				{Type: "Ready", Status: corev1.ConditionFalse},
				{Type: "NetworkReady", Status: corev1.ConditionFalse},
			}
			cond4 := []corev1.NodeCondition{
				{Type: "Ready", Status: corev1.ConditionTrue},
			}

			Expect(conditionsEqual(cond1, cond2)).To(BeTrue(), "identical conditions should be equal")
			Expect(conditionsEqual(cond1, cond3)).To(BeFalse(), "different status should not be equal")
			Expect(conditionsEqual(cond1, cond4)).To(BeFalse(), "different length should not be equal")
		})

		It("should correctly compare node taints", func() {
			taint1 := []corev1.Taint{
				{Key: "key1", Effect: corev1.TaintEffectNoSchedule, Value: "value1"},
				{Key: "key2", Effect: corev1.TaintEffectNoExecute, Value: "value2"},
			}
			taint2 := []corev1.Taint{
				{Key: "key1", Effect: corev1.TaintEffectNoSchedule, Value: "value1"},
				{Key: "key2", Effect: corev1.TaintEffectNoExecute, Value: "value2"},
			}
			taint3 := []corev1.Taint{
				{Key: "key1", Effect: corev1.TaintEffectNoSchedule, Value: "different"},
				{Key: "key2", Effect: corev1.TaintEffectNoExecute, Value: "value2"},
			}
			taint4 := []corev1.Taint{
				{Key: "key1", Effect: corev1.TaintEffectNoSchedule, Value: "value1"},
			}

			Expect(taintsEqual(taint1, taint2)).To(BeTrue(), "identical taints should be equal")
			Expect(taintsEqual(taint1, taint3)).To(BeFalse(), "different value should not be equal")
			Expect(taintsEqual(taint1, taint4)).To(BeFalse(), "different length should not be equal")
		})

		It("should correctly compare node labels", func() {
			labels1 := map[string]string{"env": "prod", "app": "web"}
			labels2 := map[string]string{"env": "prod", "app": "web"}
			labels3 := map[string]string{"env": "dev", "app": "web"}
			labels4 := map[string]string{"env": "prod"}

			Expect(labelsEqual(labels1, labels2)).To(BeTrue(), "identical labels should be equal")
			Expect(labelsEqual(labels1, labels3)).To(BeFalse(), "different value should not be equal")
			Expect(labelsEqual(labels1, labels4)).To(BeFalse(), "different length should not be equal")
		})
	})

	// Reconciliation tests need cluster resources
	Context("when reconciling a node", func() {
		var (
			ctx                 context.Context
			readinessController *RuleReadinessController
			nodeReconciler      *NodeReconciler
			fakeClientset       *fake.Clientset
			node                *corev1.Node
			rule                *nodereadinessiov1alpha1.NodeReadinessRule
			namespacedName      types.NamespacedName
		)

		BeforeEach(func() {
			ctx = context.Background()

			fakeClientset = fake.NewSimpleClientset()
			readinessController = &RuleReadinessController{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				clientset: fakeClientset,
				ruleCache: make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
			}

			nodeReconciler = &NodeReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Controller: readinessController,
			}
			namespacedName = types.NamespacedName{Name: nodeName}

			node = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nodeName,
					Labels: map[string]string{"env": "test"},
				},
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{Key: taintKey, Effect: corev1.TaintEffectNoSchedule},
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: conditionType, Status: corev1.ConditionFalse},
					},
				},
			}

			rule = &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: ruleName,
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: conditionType, RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    taintKey,
						Effect: corev1.TaintEffectNoSchedule,
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "test"},
					},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
				},
			}
		})

		JustBeforeEach(func() {
			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			// Manually add rule to cache to simulate RuleReconciler
			readinessController.updateRuleCache(ctx, rule)
		})

		AfterEach(func() {
			// Delete node first
			_ = k8sClient.Delete(ctx, node)

			// Remove finalizers and delete rule
			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: ruleName}, updatedRule); err == nil {
				updatedRule.Finalizers = nil
				_ = k8sClient.Update(ctx, updatedRule)
				_ = k8sClient.Delete(ctx, updatedRule)
			}

			// Wait for deletion to complete before next test
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: ruleName}, &nodereadinessiov1alpha1.NodeReadinessRule{})
				return apierrors.IsNotFound(err)
			}, time.Second*10).Should(BeTrue())

			// Remove rule from cache
			readinessController.removeRuleFromCache(ctx, ruleName)
		})

		When("in bootstrap-only mode", func() {
			BeforeEach(func() {
				rule.Spec.EnforcementMode = nodereadinessiov1alpha1.EnforcementModeBootstrapOnly
			})

			It("should remove the taint when conditions are met", func() {
				// Initial state: taint exists
				Expect(node.Spec.Taints).ToNot(BeEmpty())

				// Update condition to be satisfied
				node.Status.Conditions[0].Status = corev1.ConditionTrue
				Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())

				// Reconcile
				_, err := nodeReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
				Expect(err).NotTo(HaveOccurred())

				// Verify taint is removed
				Eventually(func() bool {
					updatedNode := &corev1.Node{}
					_ = k8sClient.Get(ctx, namespacedName, updatedNode)
					for _, taint := range updatedNode.Spec.Taints {
						if taint.Key == taintKey {
							return true
						}
					}
					return false
				}, time.Second*5).Should(BeFalse())

				// Verify bootstrap completion annotation is added
				Eventually(func() map[string]string {
					updatedNode := &corev1.Node{}
					_ = k8sClient.Get(ctx, namespacedName, updatedNode)
					return updatedNode.Annotations
				}).Should(HaveKey("readiness.k8s.io/bootstrap-completed-" + ruleName))
			})

			It("should not re-add the taint if conditions regress after completion", func() {
				// Step 1: Meet conditions and remove taint
				node.Status.Conditions[0].Status = corev1.ConditionTrue
				Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())
				_, err := nodeReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
				Expect(err).NotTo(HaveOccurred())
				Eventually(func() bool {
					updatedNode := &corev1.Node{}
					_ = k8sClient.Get(ctx, namespacedName, updatedNode)
					for _, taint := range updatedNode.Spec.Taints {
						if taint.Key == taintKey {
							return true
						}
					}
					return false
				}, time.Second*5).Should(BeFalse())

				// Step 2: Regress conditions
				updatedNode := &corev1.Node{}
				Expect(k8sClient.Get(ctx, namespacedName, updatedNode)).To(Succeed())
				updatedNode.Status.Conditions[0].Status = corev1.ConditionFalse
				Expect(k8sClient.Status().Update(ctx, updatedNode)).To(Succeed())

				// Reconcile again
				_, err = nodeReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
				Expect(err).NotTo(HaveOccurred())

				// Verify taint is NOT re-added
				Consistently(func() bool {
					recheckedNode := &corev1.Node{}
					_ = k8sClient.Get(ctx, namespacedName, recheckedNode)
					for _, taint := range recheckedNode.Spec.Taints {
						if taint.Key == taintKey {
							return true
						}
					}
					return false
				}, time.Second*2).Should(BeFalse())
			})
		})

		When("in continuous mode", func() {
			BeforeEach(func() {
				rule.Spec.EnforcementMode = nodereadinessiov1alpha1.EnforcementModeContinuous
			})

			It("should remove the taint when conditions are met", func() {
				node.Status.Conditions[0].Status = corev1.ConditionTrue
				Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())

				_, err := nodeReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() bool {
					updatedNode := &corev1.Node{}
					_ = k8sClient.Get(ctx, namespacedName, updatedNode)
					for _, taint := range updatedNode.Spec.Taints {
						if taint.Key == taintKey {
							return true
						}
					}
					return false
				}, time.Second*5).Should(BeFalse())
			})

			It("should re-add the taint if conditions regress", func() {
				// Step 1: Meet conditions and remove taint
				node.Status.Conditions[0].Status = corev1.ConditionTrue
				Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())
				_, err := nodeReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
				Expect(err).NotTo(HaveOccurred())
				Eventually(func() bool {
					updatedNode := &corev1.Node{}
					_ = k8sClient.Get(ctx, namespacedName, updatedNode)
					for _, taint := range updatedNode.Spec.Taints {
						if taint.Key == taintKey {
							return true
						}
					}
					return false
				}, time.Second*5).Should(BeFalse())

				// Step 2: Regress conditions
				updatedNode := &corev1.Node{}
				Expect(k8sClient.Get(ctx, namespacedName, updatedNode)).To(Succeed())
				updatedNode.Status.Conditions[0].Status = corev1.ConditionFalse
				Expect(k8sClient.Status().Update(ctx, updatedNode)).To(Succeed())

				// Reconcile again
				_, err = nodeReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
				Expect(err).NotTo(HaveOccurred())

				// Verify taint IS re-added
				Eventually(func() bool {
					recheckedNode := &corev1.Node{}
					_ = k8sClient.Get(ctx, namespacedName, recheckedNode)
					for _, taint := range recheckedNode.Spec.Taints {
						if taint.Key == taintKey {
							return true
						}
					}
					return false
				}, time.Second*5).Should(BeTrue())
			})
		})

		When("a rule's node selector does not match", func() {
			BeforeEach(func() {
				rule.Spec.NodeSelector.MatchLabels = map[string]string{"env": "non-existent"}
			})

			It("should not remove the taint", func() {
				node.Status.Conditions[0].Status = corev1.ConditionTrue
				Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())

				_, err := nodeReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
				Expect(err).NotTo(HaveOccurred())

				Consistently(func() []corev1.Taint {
					updatedNode := &corev1.Node{}
					_ = k8sClient.Get(ctx, namespacedName, updatedNode)
					return updatedNode.Spec.Taints
				}, time.Second*2).ShouldNot(BeEmpty())
			})
		})
	})

	// Test for rule deletion race condition
	Context("when processing nodes during rule deletion", func() {

		var (
			ctx                 context.Context
			readinessController *RuleReadinessController
			nodeReconciler      *NodeReconciler
			fakeClientset       *fake.Clientset
			node                *corev1.Node
			rule                *nodereadinessiov1alpha1.NodeReadinessRule
			namespacedName      types.NamespacedName
		)

		BeforeEach(func() {
			ctx = context.Background()

			fakeClientset = fake.NewSimpleClientset()
			readinessController = &RuleReadinessController{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				clientset: fakeClientset,
				ruleCache: make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
			}

			nodeReconciler = &NodeReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Controller: readinessController,
			}
			namespacedName = types.NamespacedName{Name: nodeName}

			rule = &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: ruleName,
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: conditionType, RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    taintKey,
						Effect: corev1.TaintEffectNoSchedule,
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "test"},
					},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
				},
			}

			node = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nodeName,
					Labels: map[string]string{"env": "test"},
				},
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						// Set expected condition to False - would normally trigger to set taint if rule is active
						{Type: conditionType, Status: corev1.ConditionFalse},
					},
				},
			}
		})

		JustBeforeEach(func() {
			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
		})

		AfterEach(func() {
			// Delete node first
			_ = k8sClient.Delete(ctx, node)

			// Remove finalizers and delete rule
			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: ruleName}, updatedRule); err == nil {
				updatedRule.Finalizers = nil
				_ = k8sClient.Update(ctx, updatedRule)
				_ = k8sClient.Delete(ctx, updatedRule)
			}

			// Wait for deletion to complete before next test
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: ruleName}, &nodereadinessiov1alpha1.NodeReadinessRule{})
				return apierrors.IsNotFound(err)
			}, time.Second*10).Should(BeTrue())

			// Remove rule from cache
			readinessController.removeRuleFromCache(ctx, ruleName)
		})

		It("should not add taints when rule has DeletionTimestamp set", func() {
			// mark rule for deletion
			By("Creating rule with DeletionTimestamp")
			Expect(k8sClient.Delete(ctx, rule)).To(Succeed())
			deletingRule := rule.DeepCopy()
			now := metav1.Now()
			deletingRule.DeletionTimestamp = &now
			readinessController.updateRuleCache(ctx, deletingRule)

			By("Triggering NodeReconciler") // should skip because rule is being deleted
			_, err := nodeReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying no taint was added")
			finalNode := &corev1.Node{}
			Expect(k8sClient.Get(ctx, namespacedName, finalNode)).To(Succeed())

			hasTaint := false
			for _, t := range finalNode.Spec.Taints {
				if t.Key == taintKey {
					hasTaint = true
					break
				}
			}
			Expect(hasTaint).To(BeFalse(),
				"Taint should not be added when rule is being deleted")
		})

		It("should skip rule evaluation completely when DeletionTimestamp is set", func() {
			By("Creating rule with DeletionTimestamp")
			deletingRule := rule.DeepCopy()
			now := metav1.Now()
			deletingRule.DeletionTimestamp = &now
			readinessController.updateRuleCache(ctx, deletingRule)

			By("Triggering reconciliation")
			_, err := nodeReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying rule was not evaluated")
			// Check that no NodeEvaluation was added for this node
			checkRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ruleName}, checkRule)).To(Succeed())

			hasEval := false
			for _, eval := range checkRule.Status.NodeEvaluations {
				if eval.NodeName == nodeName {
					hasEval = true
					break
				}
			}
			Expect(hasEval).To(BeFalse(),
				"Rule with DeletionTimestamp should not create node evaluation")
		})
	})
})
