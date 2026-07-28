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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nodereadinessiov1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

var _ = Describe("Node Controller Reproduction", func() {
	Context("when reconciling a node with a very long rule name", func() {
		var (
			ctx                 context.Context
			readinessController *RuleReadinessController
			nodeReconciler      *NodeReconciler
			fakeClientset       *fake.Clientset
			node                *corev1.Node
			rule                *nodereadinessiov1alpha1.NodeReadinessRule
			longRuleName        = "my-very-long-rule-name-that-exceeds-the-annotation-key-limit-strictly"
		)

		BeforeEach(func() {
			ctx = context.Background()

			fakeClientset = fake.NewSimpleClientset()
			readinessController = &RuleReadinessController{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				clientset:     fakeClientset,
				ruleCache:     make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
				EventRecorder: events.NewFakeRecorder(10),
			}

			nodeReconciler = &NodeReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Controller: readinessController,
			}

			node = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "repro-node",
					Labels: map[string]string{"env": "repro"},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: "ReproCondition", Status: corev1.ConditionTrue},
					},
				},
			}

			rule = &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: longRuleName,
				},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
						{Type: "ReproCondition", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "readiness.k8s.io/repro-taint",
						Effect: corev1.TaintEffectNoSchedule,
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "repro"},
					},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeBootstrapOnly,
				},
			}
		})

		JustBeforeEach(func() {
			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
			readinessController.updateRuleCache(ctx, rule)
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, node)
			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: longRuleName}, updatedRule); err == nil {
				updatedRule.Finalizers = nil
				_ = k8sClient.Update(ctx, updatedRule)
				_ = k8sClient.Delete(ctx, updatedRule)
			}
			readinessController.removeRuleFromCache(ctx, longRuleName)
		})

		It("should successfully mark bootstrap completed using the UID-based annotation key for long rule names", func() {
			// Trigger reconciliation
			_, err := nodeReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "repro-node"}})
			Expect(err).NotTo(HaveOccurred())

			recheckedNode := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "repro-node"}, recheckedNode)).To(Succeed())

			// The bootstrap annotation key uses the rule's UID as the suffix,
			// so even very long rule names never violate the 63-char key limit.
			uidKey := bootstrapAnnotationKey(rule.GetUID())
			Expect(recheckedNode.Annotations).To(HaveKey(uidKey),
				"UID-based bootstrap annotation should be present on the node")

			// The annotation value should contain the full rule name for readability.
			Expect(recheckedNode.Annotations[uidKey]).To(ContainSubstring(longRuleName),
				"annotation value should contain the full rule name for human readability")
		})
	})

})
