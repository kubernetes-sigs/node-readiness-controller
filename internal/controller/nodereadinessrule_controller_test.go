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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nodereadinessiov1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

const (
	selectorChangeTaintKey = "readiness.k8s.io/selector-change-taint"
)

var _ = Describe("NodeReadinessRule Controller", func() {
	var (
		ctx                 context.Context
		readinessController *RuleReadinessController
		ruleReconciler      *RuleReconciler
		nodeReconciler      *NodeReconciler
		scheme              *runtime.Scheme
		fakeClientset       *fake.Clientset
		recorder            *record.FakeRecorder
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(nodereadinessiov1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClientset = fake.NewSimpleClientset()
		recorder = record.NewFakeRecorder(32)
		readinessController = &RuleReadinessController{
			Client:    k8sClient,
			Scheme:    scheme,
			clientset: fakeClientset,
			ruleCache: make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
			recorder:  recorder,
		}

		ruleReconciler = &RuleReconciler{
			Client:     k8sClient,
			Scheme:     scheme,
			Controller: readinessController,
		}

		nodeReconciler = &NodeReconciler{
			Client:     k8sClient,
			Scheme:     scheme,
			Controller: readinessController,
		}
	})

	Context("Rule Reconciliation", func() {
		It("should handle rule creation and add the finalizer to the rule", func() {
			rule := &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-rule-finalizer",
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
					Taint: corev1.Taint{
						Key:    "readiness.k8s.io/test-taint",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			Eventually(func() error {
				_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: "test-rule-finalizer"},
				})
				return err
			}).Should(Succeed())

			// Verify finalizer is added to the rule
			Eventually(func() []string {
				updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
				_ = k8sClient.Get(ctx, types.NamespacedName{Name: "test-rule-finalizer"}, updatedRule)
				return updatedRule.Finalizers
			}, time.Second*5).Should(ConsistOf(finalizerName))

			// Cleanup
			Expect(k8sClient.Delete(ctx, rule)).To(Succeed())
		})

		It("should handle rule creation and update cache", func() {
			rule := &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-rule",
					Finalizers: []string{finalizerName},
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
					Taint: corev1.Taint{
						Key:    "readiness.k8s.io/test-taint",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			Eventually(func() error {
				_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: "test-rule"},
				})
				return err
			}).Should(Succeed())

			// Verify rule is in cache
			readinessController.ruleCacheMutex.RLock()
			cachedRule, exists := readinessController.ruleCache["test-rule"]
			readinessController.ruleCacheMutex.RUnlock()
			Expect(exists).To(BeTrue())
			Expect(cachedRule.Spec.Taint.Key).To(Equal("readiness.k8s.io/test-taint"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, rule)).To(Succeed())
		})

		It("should handle rule deletion and remove from cache", func() {
			rule := &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-rule-delete",
					Finalizers: []string{finalizerName},
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
					Taint: corev1.Taint{
						Key:    "readiness.k8s.io/test-taint",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			// First reconcile to add to cache
			_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-rule-delete"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Delete the rule
			Expect(k8sClient.Delete(ctx, rule)).To(Succeed())

			// Second reconcile should remove from cache
			Eventually(func() bool {
				_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: "test-rule-delete"},
				})
				Expect(err).NotTo(HaveOccurred())

				readinessController.ruleCacheMutex.RLock()
				_, exists := readinessController.ruleCache["test-rule-delete"]
				readinessController.ruleCacheMutex.RUnlock()
				return !exists
			}).Should(BeTrue())
		})

		It("should immediately process existing nodes on rule creation", func() {
			// Create a test node first
			testNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "immediate-test-node",
					Labels: map[string]string{
						"immediate-test": "true",
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: "TestCondition", Status: corev1.ConditionFalse},
					},
				},
			}
			Expect(k8sClient.Create(ctx, testNode)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, testNode) }()

			// Now create a rule - this should immediately evaluate the existing node
			rule := &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "immediate-test-rule",
					Finalizers: []string{finalizerName},
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: "TestCondition", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "readiness.k8s.io/immediate-test-taint",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"immediate-test": "true",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, rule) }()

			// Trigger reconciliation manually to simulate CREATE event handling
			_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "immediate-test-rule"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the node gets tainted immediately due to unmet condition
			Eventually(func() bool {
				updatedNode := &corev1.Node{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "immediate-test-node"}, updatedNode)
				if err != nil {
					return false
				}
				for _, taint := range updatedNode.Spec.Taints {
					if taint.Key == "readiness.k8s.io/immediate-test-taint" && taint.Effect == corev1.TaintEffectNoSchedule {
						return true
					}
				}
				return false
			}, time.Second*5).Should(BeTrue())

			Expect(recorder.Events).To(Receive(ContainSubstring("immediate-test-node")))
		})

		It("should handle dry run mode", func() {
			rule := &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "dry-run-rule",
					Finalizers: []string{finalizerName},
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
					Taint: corev1.Taint{
						Key:    "readiness.k8s.io/dry-run-taint",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeBootstrapOnly,
					DryRun:          true,
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "dry-run-rule"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify dry run results are populated
			Eventually(func() nodereadinessiov1alpha1.DryRunResults {
				updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "dry-run-rule"}, updatedRule)
				if err != nil {
					return nodereadinessiov1alpha1.DryRunResults{}
				}
				return updatedRule.Status.DryRunResults
			}).ShouldNot(BeZero())

			// Cleanup
			Expect(k8sClient.Delete(ctx, rule)).To(Succeed())
		})
	})

	Context("Node Processing", func() {
		var testNode *corev1.Node

		BeforeEach(func() {
			testNode = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"node-role.kubernetes.io/worker": "",
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   "Ready",
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, testNode)).To(Succeed())
		})

		AfterEach(func() {
			if testNode != nil {
				_ = k8sClient.Delete(ctx, testNode)
			}
		})

		It("should process node changes", func() {
			rule := &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "node-test-rule",
					Finalizers: []string{finalizerName},
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "readiness.k8s.io/node-test-taint",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeBootstrapOnly,
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, rule) }()

			// First add rule to cache
			readinessController.updateRuleCache(ctx, rule)

			// Process node
			_, err := nodeReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-node"},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Core Logic Tests", func() {
		It("should evaluate conditions correctly", func() {
			node := &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: "Ready", Status: corev1.ConditionTrue},
						{Type: "NetworkReady", Status: corev1.ConditionFalse},
					},
				},
			}

			// Test condition exists and matches
			status := readinessController.getConditionStatus(node, "Ready")
			Expect(status).To(Equal(corev1.ConditionTrue))

			// Test condition exists but doesn't match
			status = readinessController.getConditionStatus(node, "NetworkReady")
			Expect(status).To(Equal(corev1.ConditionFalse))

			// Test missing condition
			status = readinessController.getConditionStatus(node, "StorageReady")
			Expect(status).To(Equal(corev1.ConditionUnknown))
		})

		It("should detect taints correctly", func() {
			node := &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{Key: "readiness.k8s.io/test-key", Effect: corev1.TaintEffectNoSchedule, Value: "test-value"},
						{Key: "readiness.k8s.io/another-key", Effect: corev1.TaintEffectNoExecute},
					},
				},
			}

			taintSpec := corev1.Taint{
				Key:    "readiness.k8s.io/test-key",
				Effect: corev1.TaintEffectNoSchedule,
			}

			hasTaint := readinessController.hasTaintBySpec(node, taintSpec)
			Expect(hasTaint).To(BeTrue())

			// Test non-existent taint
			nonExistentTaint := corev1.Taint{
				Key:    "readiness.k8s.io/missing-key",
				Effect: corev1.TaintEffectNoSchedule,
			}

			hasTaint = readinessController.hasTaintBySpec(node, nonExistentTaint)
			Expect(hasTaint).To(BeFalse())
		})

		It("should check rule applicability correctly", func() {
			rule := &nodereadinessiov1alpha1.NodeReadinessRule{
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
				},
			}

			// Node that matches
			matchingNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"node-role.kubernetes.io/worker": "",
					},
				},
			}

			applies := readinessController.ruleAppliesTo(ctx, rule, matchingNode)
			Expect(applies).To(BeTrue())

			// Node that doesn't match
			nonMatchingNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"node-role.kubernetes.io/control-plane": "",
					},
				},
			}

			applies = readinessController.ruleAppliesTo(ctx, rule, nonMatchingNode)
			Expect(applies).To(BeFalse())

			// Rule without selector should apply to all nodes
			ruleWithoutSelector := &nodereadinessiov1alpha1.NodeReadinessRule{
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{},
			}

			applies = readinessController.ruleAppliesTo(ctx, ruleWithoutSelector, nonMatchingNode)
			Expect(applies).To(BeTrue())
		})

		It("should handle bootstrap completion tracking", func() {
			nodeName := "bootstrap-test-node"
			ruleName := "bootstrap-test-rule"

			// Initially not completed
			completed := readinessController.isBootstrapCompleted(nodeName, ruleName)
			Expect(completed).To(BeFalse())

			// Create a node for testing
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName,
				},
			}
			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, node) }()

			// Mark as completed
			readinessController.markBootstrapCompleted(ctx, nodeName, ruleName)

			// Should now be completed
			Eventually(func() bool {
				return readinessController.isBootstrapCompleted(nodeName, ruleName)
			}).Should(BeTrue())
		})
	})

	Context("when a new rule is created", func() {
		var node *corev1.Node
		var rule *nodereadinessiov1alpha1.NodeReadinessRule

		BeforeEach(func() {
			node = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"app": "backend"}},
				Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: "DBReady", Status: corev1.ConditionFalse}}},
			}
			rule = &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "db-rule",
					Finalizers: []string{finalizerName},
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions:      []nodereadinessiov1alpha1.ConditionRequirement{{Type: "DBReady", RequiredStatus: corev1.ConditionTrue}},
					Taint:           corev1.Taint{Key: "readiness.k8s.io/db-unready", Effect: corev1.TaintEffectNoSchedule},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
					NodeSelector:    metav1.LabelSelector{MatchLabels: map[string]string{"app": "backend"}},
				},
			}
			Expect(k8sClient.Create(ctx, node)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, node)).To(Succeed())
			Expect(k8sClient.Delete(ctx, rule)).To(Succeed())
		})

		It("should evaluate the rule against existing nodes and add taints if necessary", func() {
			// Create the rule
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			// Reconcile the rule
			_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "db-rule"}})
			Expect(err).NotTo(HaveOccurred())

			// Verify that the taint has been added to the node
			Eventually(func() bool {
				updatedNode := &corev1.Node{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "node1"}, updatedNode); err != nil {
					return false
				}
				for _, taint := range updatedNode.Spec.Taints {
					if taint.Key == rule.Spec.Taint.Key && taint.Effect == rule.Spec.Taint.Effect {
						return true
					}
				}
				return false
			}, time.Second*5).Should(BeTrue())

			// Verify the rule applied to the node.
			Expect(recorder.Events).To(Receive(ContainSubstring("node1")))
		})
	})

	Context("when an existing rule is updated", func() {
		var rule *nodereadinessiov1alpha1.NodeReadinessRule

		BeforeEach(func() {
			rule = &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "metadata-gen-test-rule",
					Finalizers: []string{finalizerName},
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "readiness.k8s.io/metadata-gen-test-taint",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "metadata-gen-test-rule"},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "metadata-gen-test-rule"}, updatedRule); err == nil {
				updatedRule.Finalizers = nil
				_ = k8sClient.Update(ctx, updatedRule)
				_ = k8sClient.Delete(ctx, updatedRule)
			}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "metadata-gen-test-rule"}, &nodereadinessiov1alpha1.NodeReadinessRule{})
				return apierrors.IsNotFound(err)
			}, time.Second*10).Should(BeTrue())
		})

		It("should not update ObservedGeneration for metadata-only changes", func() {
			createdRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "metadata-gen-test-rule"}, createdRule)).To(Succeed())
			initialObservedGeneration := createdRule.Status.ObservedGeneration
			initialGeneration := createdRule.Generation

			patch := client.MergeFrom(createdRule.DeepCopy())
			createdRule.Labels = map[string]string{"test-label": "value"}
			Expect(k8sClient.Patch(ctx, createdRule, patch)).To(Succeed())

			_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "metadata-gen-test-rule"},
			})
			Expect(err).NotTo(HaveOccurred())

			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "metadata-gen-test-rule"}, updatedRule)).To(Succeed())

			Expect(updatedRule.Generation).To(Equal(initialGeneration))
			Expect(updatedRule.Status.ObservedGeneration).To(Equal(initialObservedGeneration))
		})
	})

	Context("when a new node is added", func() {
		var rule *nodereadinessiov1alpha1.NodeReadinessRule
		var newNode *corev1.Node

		BeforeEach(func() {
			rule = &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "new-node-rule",
					Finalizers: []string{finalizerName},
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions:      []nodereadinessiov1alpha1.ConditionRequirement{{Type: "TestReady", RequiredStatus: corev1.ConditionTrue}},
					Taint:           corev1.Taint{Key: "readiness.k8s.io/test-unready", Effect: corev1.TaintEffectNoSchedule},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
					NodeSelector:    metav1.LabelSelector{MatchLabels: map[string]string{"node-group": "new-workers"}},
				},
			}
			newNode = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "new-node",
					Labels: map[string]string{"node-group": "new-workers"},
				},
				Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: "TestReady", Status: corev1.ConditionFalse}}},
			}
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, rule)).To(Succeed())
			Expect(k8sClient.Delete(ctx, newNode)).To(Succeed())
		})

		It("should trigger reconciliation for existing rules", func() {
			// Create the new node, which should trigger the watch
			Expect(k8sClient.Create(ctx, newNode)).To(Succeed())

			// Add the rule to the cache
			readinessController.updateRuleCache(ctx, rule)

			// Manually trigger rule reconciliation to simulate watch behavior
			_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "new-node-rule"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify that the new node gets tainted
			Eventually(func() bool {
				updatedNode := &corev1.Node{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "new-node"}, updatedNode)
				if err != nil {
					return false
				}
				for _, taint := range updatedNode.Spec.Taints {
					if taint.Key == rule.Spec.Taint.Key {
						return true
					}
				}
				return false
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())
		})
	})

	Context("when a rule is deleted", func() {
		var rule *nodereadinessiov1alpha1.NodeReadinessRule
		var testNode *corev1.Node

		BeforeEach(func() {
			testNode = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cleanup-test-node",
					Labels: map[string]string{
						"kubernetes.io/hostname": "cleanup-test-node",
					},
				},
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{Key: "readiness.k8s.io/cleanup-taint", Effect: corev1.TaintEffectNoSchedule, Value: "pending"},
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{{Type: "TestReady", Status: corev1.ConditionFalse}},
				},
			}
			rule = &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cleanup-rule",
					Finalizers: []string{finalizerName},
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions:      []nodereadinessiov1alpha1.ConditionRequirement{{Type: "TestReady", RequiredStatus: corev1.ConditionTrue}},
					NodeSelector:    metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "cleanup-test-node"}},
					Taint:           corev1.Taint{Key: "readiness.k8s.io/cleanup-taint", Effect: corev1.TaintEffectNoSchedule},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
				},
			}

			Expect(k8sClient.Create(ctx, testNode)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, testNode)
			_ = k8sClient.Delete(ctx, rule)
		})

		It("should remove taints from nodes when rule is deleted", func() {
			// Initial reconcile to add finalizer
			_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "cleanup-rule"}})
			Expect(err).NotTo(HaveOccurred())

			// Verify finalizer was added
			Eventually(func() []string {
				updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
				_ = k8sClient.Get(ctx, types.NamespacedName{Name: "cleanup-rule"}, updatedRule)
				return updatedRule.Finalizers
			}, time.Second*5).Should(ContainElement("readiness.node.x-k8s.io/cleanup-taints"))

			// Verify node still has taint
			updatedNode := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cleanup-test-node"}, updatedNode)).To(Succeed())
			hasTaint := false
			for _, taint := range updatedNode.Spec.Taints {
				if taint.Key == "readiness.k8s.io/cleanup-taint" {
					hasTaint = true
					break
				}
			}
			Expect(hasTaint).To(BeTrue(), "Node should have taint before rule deletion")

			// Delete the rule
			Expect(k8sClient.Delete(ctx, rule)).To(Succeed())

			// Trigger reconciliation to process deletion
			_, err = ruleReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "cleanup-rule"}})
			Expect(err).NotTo(HaveOccurred())

			// Verify taint is removed from node
			Eventually(func() bool {
				updatedNode := &corev1.Node{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cleanup-test-node"}, updatedNode); err != nil {
					return false
				}
				for _, taint := range updatedNode.Spec.Taints {
					if taint.Key == "readiness.k8s.io/cleanup-taint" {
						return false // Taint still exists
					}
				}
				return true // Taint removed
			}, time.Second*10).Should(BeTrue(), "Taint should be removed after rule deletion")

			// Verify rule is actually deleted (finalizer removed)
			Eventually(func() bool {
				deletedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "cleanup-rule"}, deletedRule)
				return err != nil && client.IgnoreNotFound(err) == nil
			}, time.Second*10).Should(BeTrue(), "Rule should be fully deleted")
		})
	})

	Context("when a node is deleted", func() {
		var rule *nodereadinessiov1alpha1.NodeReadinessRule
		var node1, node2 *corev1.Node

		BeforeEach(func() {
			rule = &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "delete-node-rule",
					Finalizers: []string{finalizerName}},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{{Type: "Ready", RequiredStatus: corev1.ConditionTrue}},
					NodeSelector: metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "kubernetes.io/hostname",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"node1", "node2"},
							},
						},
					},
					Taint:           corev1.Taint{Key: "readiness.k8s.io/unready", Effect: corev1.TaintEffectNoSchedule},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
				},
			}
			node1 = &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"kubernetes.io/hostname": "node1"}}}
			node2 = &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: map[string]string{"kubernetes.io/hostname": "node2"}}}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
			Expect(k8sClient.Create(ctx, node1)).To(Succeed())
			Expect(k8sClient.Create(ctx, node2)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, rule)).To(Succeed())
			// node1 is already deleted in the test
			_ = k8sClient.Delete(ctx, node2)
		})
	})

	Context("when a rule's nodeSelector changes", func() {
		var rule *nodereadinessiov1alpha1.NodeReadinessRule
		var prodNode, devNode *corev1.Node

		BeforeEach(func() {
			prodNode = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "prod-node",
					Labels: map[string]string{"env": "prod"},
				},
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{Key: selectorChangeTaintKey, Effect: corev1.TaintEffectNoSchedule, Value: "pending"},
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{{Type: "TestReady", Status: corev1.ConditionFalse}},
				},
			}

			devNode = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "dev-node",
					Labels: map[string]string{"env": "dev"},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{{Type: "TestReady", Status: corev1.ConditionFalse}},
				},
			}

			rule = &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "selector-change-rule",
					Finalizers: []string{finalizerName},
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: "TestReady", RequiredStatus: corev1.ConditionTrue},
					},
					Taint:           corev1.Taint{Key: selectorChangeTaintKey, Effect: corev1.TaintEffectNoSchedule},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "prod"},
					},
				},
			}

			Expect(k8sClient.Create(ctx, prodNode)).To(Succeed())
			Expect(k8sClient.Create(ctx, devNode)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, prodNode)
			_ = k8sClient.Delete(ctx, devNode)
			_ = k8sClient.Delete(ctx, rule)
		})

		It("should remove taints from nodes that no longer match the selector", func() {
			// Initial reconcile - prod node should be managed, dev node should not
			_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "selector-change-rule"}})
			Expect(err).NotTo(HaveOccurred())

			// Verify prod node still has taint (condition not met)
			Eventually(func() bool {
				updatedNode := &corev1.Node{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "prod-node"}, updatedNode); err != nil {
					return false
				}
				for _, taint := range updatedNode.Spec.Taints {
					if taint.Key == selectorChangeTaintKey {
						return true
					}
				}
				return false
			}, time.Second*5).Should(BeTrue(), "Prod node should have taint")

			// Verify dev node does not have taint (not selected)
			Consistently(func() bool {
				updatedNode := &corev1.Node{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "dev-node"}, updatedNode); err != nil {
					return false
				}
				for _, taint := range updatedNode.Spec.Taints {
					if taint.Key == selectorChangeTaintKey {
						return false // Taint found (unexpected)
					}
				}
				return true // No taint (expected)
			}, time.Second*2).Should(BeTrue(), "Dev node should not have taint")

			// Update rule to target dev nodes instead of prod nodes
			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "selector-change-rule"}, updatedRule)).To(Succeed())
			updatedRule.Spec.NodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "dev"},
			}
			Expect(k8sClient.Update(ctx, updatedRule)).To(Succeed())

			// Trigger reconciliation
			_, err = ruleReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "selector-change-rule"}})
			Expect(err).NotTo(HaveOccurred())

			// Verify taint is removed from prod node (no longer selected)
			Eventually(func() bool {
				updatedNode := &corev1.Node{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "prod-node"}, updatedNode); err != nil {
					return false
				}
				for _, taint := range updatedNode.Spec.Taints {
					if taint.Key == selectorChangeTaintKey {
						return false // Taint still exists
					}
				}
				return true // Taint removed
			}, time.Second*10).Should(BeTrue(), "Prod node taint should be removed after selector change")

			// Verify dev node now gets taint (newly selected, condition not met)
			Eventually(func() bool {
				updatedNode := &corev1.Node{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "dev-node"}, updatedNode); err != nil {
					return false
				}
				for _, taint := range updatedNode.Spec.Taints {
					if taint.Key == selectorChangeTaintKey {
						return true
					}
				}
				return false
			}, time.Second*10).Should(BeTrue(), "Dev node should now have taint after selector change")
		})
	})

	Context("when existing rule is updated", func() {
		var rule *nodereadinessiov1alpha1.NodeReadinessRule
		var node *corev1.Node

		BeforeEach(func() {
			node = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "observed-gen-test-node",
					Labels: map[string]string{"app": "test"},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: "Ready", Status: corev1.ConditionTrue},
					},
				},
			}

			rule = &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "observed-gen-test-rule",
					Finalizers: []string{finalizerName},
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "readiness.k8s.io/observed-gen-test-taint",
						Effect: corev1.TaintEffectNoSchedule,
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
				},
			}

			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, node)

			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "observed-gen-test-rule"}, updatedRule); err == nil {
				updatedRule.Finalizers = nil
				_ = k8sClient.Update(ctx, updatedRule)
				_ = k8sClient.Delete(ctx, updatedRule)
			}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "observed-gen-test-rule"}, &nodereadinessiov1alpha1.NodeReadinessRule{})
				return apierrors.IsNotFound(err)
			}, time.Second*10).Should(BeTrue())
		})

		It("should set ObservedGeneration to match rule Generation after reconciliation", func() {
			By("Running initial reconciliation")
			_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "observed-gen-test-rule"},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ObservedGeneration matches Generation")
			Eventually(func() bool {
				updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "observed-gen-test-rule"}, updatedRule); err != nil {
					return false
				}
				return updatedRule.Status.ObservedGeneration == updatedRule.Generation
			}, time.Second*5).Should(BeTrue(), "ObservedGeneration should match Generation")
		})

		It("should update ObservedGeneration when spec changes", func() {
			By("Running initial reconciliation")
			_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "observed-gen-test-rule"},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Getting initial generation")
			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "observed-gen-test-rule"}, updatedRule)).To(Succeed())
			initialGeneration := updatedRule.Generation
			Expect(updatedRule.Status.ObservedGeneration).To(Equal(initialGeneration))

			By("Updating rule spec to trigger generation increment")
			updatedRule.Spec.Taint.Value = "new-value"
			Expect(k8sClient.Update(ctx, updatedRule)).To(Succeed())

			By("Running reconciliation after spec change")
			Eventually(func() error {
				_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: "observed-gen-test-rule"},
				})
				return err
			}, time.Second*5).Should(Succeed())

			By("Verifying ObservedGeneration updated to new Generation")
			Eventually(func() bool {
				latestRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "observed-gen-test-rule"}, latestRule); err != nil {
					return false
				}
				return latestRule.Status.ObservedGeneration == latestRule.Generation &&
					latestRule.Generation > initialGeneration
			}, time.Second*5).Should(BeTrue(), "ObservedGeneration should update when Generation changes")
		})

		It("should not update ObservedGeneration for metadata-only changes", func() {
			By("Running initial reconciliation")
			_, err := ruleReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "observed-gen-test-rule"},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Getting initial generation and observedGeneration")
			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "observed-gen-test-rule"}, updatedRule)).To(Succeed())
			initialGeneration := updatedRule.Generation
			initialObservedGeneration := updatedRule.Status.ObservedGeneration
			Expect(initialObservedGeneration).To(Equal(initialGeneration))

			By("Updating only metadata (adding annotation)")
			updatedRule.Annotations = map[string]string{"test": "value"}
			Expect(k8sClient.Update(ctx, updatedRule)).To(Succeed())

			By("Verifying Generation did not change")
			Eventually(func() int64 {
				latestRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
				_ = k8sClient.Get(ctx, types.NamespacedName{Name: "observed-gen-test-rule"}, latestRule)
				return latestRule.Generation
			}, time.Second*2).Should(Equal(initialGeneration), "Generation should not change for metadata updates")

			By("Running reconciliation after metadata change")
			_, err = ruleReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "observed-gen-test-rule"},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ObservedGeneration remains unchanged")
			latestRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "observed-gen-test-rule"}, latestRule)).To(Succeed())
			Expect(latestRule.Status.ObservedGeneration).To(Equal(initialObservedGeneration),
				"ObservedGeneration should not change when Generation doesn't change")
		})
	})

	Context("when applied nodes for a rule are changed", func() {
		var rule *nodereadinessiov1alpha1.NodeReadinessRule
		var node1, node2, node3 *corev1.Node

		BeforeEach(func() {
			node1 = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "applied-node-1",
					Labels: map[string]string{"group": "applied"},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{{Type: "Ready", Status: corev1.ConditionTrue}},
				},
			}
			node2 = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "applied-node-2",
					Labels: map[string]string{"group": "applied"},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{{Type: "Ready", Status: corev1.ConditionFalse}},
				},
			}
			node3 = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "applied-node-3",
					Labels: map[string]string{"group": "other"},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{{Type: "Ready", Status: corev1.ConditionTrue}},
				},
			}

			rule = &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "applied-nodes-rule",
					Finalizers: []string{finalizerName},
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "readiness.k8s.io/applied-test-taint",
						Effect: corev1.TaintEffectNoSchedule,
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"group": "applied"},
					},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
				},
			}

			Expect(k8sClient.Create(ctx, node1)).To(Succeed())
			Expect(k8sClient.Create(ctx, node2)).To(Succeed())
			Expect(k8sClient.Create(ctx, node3)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, node1)
			_ = k8sClient.Delete(ctx, node2)
			_ = k8sClient.Delete(ctx, node3)

			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "applied-nodes-rule"}, updatedRule); err == nil {
				updatedRule.Finalizers = nil
				_ = k8sClient.Update(ctx, updatedRule)
				_ = k8sClient.Delete(ctx, updatedRule)
			}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "applied-nodes-rule"}, &nodereadinessiov1alpha1.NodeReadinessRule{})
				return apierrors.IsNotFound(err)
			}, time.Second*10).Should(BeTrue())
		})
	})
})
