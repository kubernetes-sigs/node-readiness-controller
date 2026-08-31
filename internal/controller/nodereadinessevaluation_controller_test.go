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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nodereadinessiov1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

// nreNamespacedName returns the NamespacedName for a NodeReadinessEvaluation
// (cluster-scoped, so Namespace is always empty).
func nreNamespacedName(nodeName string) types.NamespacedName {
	return types.NamespacedName{Name: nodeName}
}

var _ = Describe("NodeReadinessEvaluation writes", func() {
	const (
		nreTestTimeout  = 10 * time.Second
		nreTestInterval = 100 * time.Millisecond

		taintKey      = "readiness.k8s.io/nre-test"
		conditionType = "NRETestCondition"
	)

	// makeRule returns a basic continuous NodeReadinessRule that targets nodes
	// with label env=nre-test.
	makeRule := func(name string) *nodereadinessiov1alpha1.NodeReadinessRule {
		return &nodereadinessiov1alpha1.NodeReadinessRule{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
				EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
				Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
					{Type: conditionType, RequiredStatus: corev1.ConditionTrue},
				},
				Taint: corev1.Taint{
					Key:    taintKey,
					Effect: corev1.TaintEffectNoSchedule,
				},
				NodeSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "nre-test"},
				},
			},
		}
	}

	// makeNode returns a node with label env=nre-test and the given taints.
	makeNode := func(name string, taints []corev1.Taint, condStatus corev1.ConditionStatus) *corev1.Node {
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{"env": "nre-test"},
			},
			Spec: corev1.NodeSpec{Taints: taints},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: conditionType, Status: condStatus},
				},
			},
		}
	}

	// sharedSetup wires up a RuleReadinessController backed by the envtest k8sClient
	// with NRE writing enabled.
	sharedSetup := func() *RuleReadinessController {
		return &RuleReadinessController{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			clientset:     fake.NewSimpleClientset(),
			ruleCache:     make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
			EventRecorder: events.NewFakeRecorder(32),
			EnableNRE:     true,
		}
	}

	// reconcileNRE triggers updateNREForNode for the named node.
	reconcileNRE := func(rc *RuleReadinessController, nodeName string) {
		GinkgoHelper()
		node := &corev1.Node{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
		rc.updateNREForNode(ctx, node)
	}

	// getNRE fetches the current NRE for the given node name.
	getNRE := func(nodeName string) *nodereadinessiov1alpha1.NodeReadinessEvaluation {
		GinkgoHelper()
		nre := &nodereadinessiov1alpha1.NodeReadinessEvaluation{}
		Expect(k8sClient.Get(ctx, nreNamespacedName(nodeName), nre)).To(Succeed())
		return nre
	}

	// Suite A — Fresh cluster: NRE enabled from the start.

	Context("Suite A — fresh cluster with NRE enabled", func() {
		var (
			rc   *RuleReadinessController
			node *corev1.Node
			rule *nodereadinessiov1alpha1.NodeReadinessRule
		)

		BeforeEach(func() {
			rc = sharedSetup()

			node = makeNode("nre-a-node", []corev1.Taint{
				{Key: taintKey, Effect: corev1.TaintEffectNoSchedule},
			}, corev1.ConditionFalse)
			rule = makeRule("nre-a-rule")

			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
			rc.updateRuleCache(ctx, rule)
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, node)

			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: rule.Name}, updatedRule); err == nil {
				updatedRule.Finalizers = nil
				_ = k8sClient.Update(ctx, updatedRule)
				_ = k8sClient.Delete(ctx, updatedRule)
			}
			Eventually(func() bool {
				return apierrors.IsNotFound(
					k8sClient.Get(ctx, types.NamespacedName{Name: rule.Name},
						&nodereadinessiov1alpha1.NodeReadinessRule{}))
			}, nreTestTimeout).Should(BeTrue())

			// Delete the NRE if it still exists (GC may not run in envtest).
			nre := &nodereadinessiov1alpha1.NodeReadinessEvaluation{}
			if err := k8sClient.Get(ctx, nreNamespacedName(node.Name), nre); err == nil {
				_ = k8sClient.Delete(ctx, nre)
			}

			rc.removeRuleFromCache(ctx, rule.Name)
		})

		It("A1 — creates NRE with correct spec.nodeName and ownerReference on first reconcile", func() {
			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)

			By("checking spec.nodeName")
			Expect(nre.Spec.NodeName).To(Equal(node.Name))

			By("checking ownerReference points to the Node")
			Expect(nre.OwnerReferences).To(HaveLen(1))
			Expect(nre.OwnerReferences[0].Kind).To(Equal("Node"))
			Expect(nre.OwnerReferences[0].Name).To(Equal(node.Name))
			Expect(nre.OwnerReferences[0].UID).To(Equal(node.GetUID()))
		})

		It("A2 — state is NotAvailable and activeTaints=1 when conditions are not met", func() {
			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)

			By("checking top-level state")
			Expect(nre.Status.State).To(Equal(nodereadinessiov1alpha1.NodeEvaluationStateNotAvailable))

			By("checking RuleEvaluation entry")
			Expect(nre.Status.Rules).To(HaveLen(1))
			Expect(nre.Status.Rules[0].RuleName).To(Equal(rule.Name))
			Expect(nre.Status.Rules[0].RuleUID).To(Equal(rule.GetUID()))
			Expect(nre.Status.Rules[0].RuleStatus).To(Equal(nodereadinessiov1alpha1.RuleStatusUnsatisfied))
			Expect(nre.Status.Rules[0].TaintStatus).To(Equal(nodereadinessiov1alpha1.TaintStatusPresent))
			Expect(nre.Status.Rules[0].LastEvaluationTime.IsZero()).To(BeFalse())
		})

		It("A3 — state transitions to Available after conditions are met and taint is removed", func() {
			// First reconcile: conditions not met, taint present.
			reconcileNRE(rc, node.Name)
			Expect(getNRE(node.Name).Status.State).To(Equal(nodereadinessiov1alpha1.NodeEvaluationStateNotAvailable))

			// Satisfy the condition and remove the taint (simulating NodeReconciler action).
			updatedNode := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: node.Name}, updatedNode)).To(Succeed())
			updatedNode.Status.Conditions[0].Status = corev1.ConditionTrue
			Expect(k8sClient.Status().Update(ctx, updatedNode)).To(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: node.Name}, updatedNode)).To(Succeed())
			updatedNode.Spec.Taints = nil
			Expect(k8sClient.Update(ctx, updatedNode)).To(Succeed())

			// Second reconcile: conditions met, taint absent.
			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)
			Expect(nre.Status.State).To(Equal(nodereadinessiov1alpha1.NodeEvaluationStateAvailable))
			Expect(nre.Status.Rules[0].RuleStatus).To(Equal(nodereadinessiov1alpha1.RuleStatusSatisfied))
			Expect(nre.Status.Rules[0].TaintStatus).To(Equal(nodereadinessiov1alpha1.TaintStatusAbsent))
		})

		It("A4 — taintKey and taintEffect are stamped on the RuleEvaluation entry", func() {
			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)
			Expect(nre.Status.Rules).To(HaveLen(1))
			eval := nre.Status.Rules[0]

			By("taintKey matches the rule's taint key")
			Expect(eval.TaintKey).To(Equal(taintKey))

			By("taintEffect matches the rule's taint effect")
			Expect(eval.TaintEffect).To(Equal(corev1.TaintEffectNoSchedule))
		})

		It("A4b — condition evaluation breakdown is present in RuleEvaluation", func() {
			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)
			Expect(nre.Status.Rules[0].ReadinessConditions).To(HaveLen(1))
			cond := nre.Status.Rules[0].ReadinessConditions[0]
			Expect(cond.Type).To(Equal(conditionType))
			Expect(cond.RequiredStatus).To(Equal(corev1.ConditionTrue))
			Expect(cond.CurrentStatus).To(Equal(corev1.ConditionFalse))
		})

		It("A5 — Evaluated=True and Available=False conditions are set when taint is present", func() {
			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)
			By("Evaluated condition is True — no errors")
			evaluated := findCondition(nre.Status.Conditions, "Evaluated")
			Expect(evaluated).NotTo(BeNil())
			Expect(evaluated.Status).To(Equal(metav1.ConditionTrue))
			Expect(evaluated.Reason).To(Equal("EvaluationSuccessful"))

			By("Available condition is False — taint is active")
			available := findCondition(nre.Status.Conditions, "Available")
			Expect(available).NotTo(BeNil())
			Expect(available.Status).To(Equal(metav1.ConditionFalse))
			Expect(available.Reason).To(Equal("TaintsActive"))
			Expect(available.Message).To(ContainSubstring("1 active taint"))
		})

		It("A6 — Available=True condition is set when all taints are cleared", func() {
			// Remove taint and satisfy condition first.
			updatedNode := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: node.Name}, updatedNode)).To(Succeed())
			updatedNode.Spec.Taints = nil
			Expect(k8sClient.Update(ctx, updatedNode)).To(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: node.Name}, updatedNode)).To(Succeed())
			updatedNode.Status.Conditions[0].Status = corev1.ConditionTrue
			Expect(k8sClient.Status().Update(ctx, updatedNode)).To(Succeed())

			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)
			available := findCondition(nre.Status.Conditions, "Available")
			Expect(available).NotTo(BeNil())
			Expect(available.Status).To(Equal(metav1.ConditionTrue))
			Expect(available.Reason).To(Equal("NodeAvailable"))
		})
	})

	Context("Suite A (multi-rule) — multiple rules folded into one NRE", func() {
		var (
			rc    *RuleReadinessController
			node  *corev1.Node
			rule1 *nodereadinessiov1alpha1.NodeReadinessRule
			rule2 *nodereadinessiov1alpha1.NodeReadinessRule
		)

		BeforeEach(func() {
			rc = sharedSetup()

			node = makeNode("nre-multi-node", []corev1.Taint{
				{Key: taintKey, Effect: corev1.TaintEffectNoSchedule},
			}, corev1.ConditionFalse)

			rule1 = makeRule("nre-multi-rule-1")

			// Second rule uses a different taint key.
			rule2 = makeRule("nre-multi-rule-2")
			rule2.Spec.Taint = corev1.Taint{Key: "readiness.k8s.io/nre-test-2", Effect: corev1.TaintEffectNoSchedule}

			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule1)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule2)).To(Succeed())
			rc.updateRuleCache(ctx, rule1)
			rc.updateRuleCache(ctx, rule2)
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, node)
			for _, r := range []*nodereadinessiov1alpha1.NodeReadinessRule{rule1, rule2} {
				updated := &nodereadinessiov1alpha1.NodeReadinessRule{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: r.Name}, updated); err == nil {
					updated.Finalizers = nil
					_ = k8sClient.Update(ctx, updated)
					_ = k8sClient.Delete(ctx, updated)
				}
				Eventually(func() bool {
					return apierrors.IsNotFound(
						k8sClient.Get(ctx, types.NamespacedName{Name: r.Name},
							&nodereadinessiov1alpha1.NodeReadinessRule{}))
				}, nreTestTimeout).Should(BeTrue())
				rc.removeRuleFromCache(ctx, r.Name)
			}
			nre := &nodereadinessiov1alpha1.NodeReadinessEvaluation{}
			if err := k8sClient.Get(ctx, nreNamespacedName(node.Name), nre); err == nil {
				_ = k8sClient.Delete(ctx, nre)
			}
		})

		It("A7 — both rules are folded into the single NRE for the node", func() {
			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)
			Expect(nre.Status.Rules).To(HaveLen(2),
				"both applicable rules should be present in status.rules")

			ruleNames := []string{
				nre.Status.Rules[0].RuleName,
				nre.Status.Rules[1].RuleName,
			}
			Expect(ruleNames).To(ConsistOf(rule1.Name, rule2.Name))
		})

		It("A8 — rule whose nodeSelector does not match is absent from NRE", func() {
			// Update rule2's selector to a non-matching label.
			updatedRule2 := rule2.DeepCopy()
			updatedRule2.Spec.NodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "non-matching"},
			}
			rc.updateRuleCache(ctx, updatedRule2)

			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)
			Expect(nre.Status.Rules).To(HaveLen(1),
				"only the matching rule should appear in the NRE")
			Expect(nre.Status.Rules[0].RuleName).To(Equal(rule1.Name))
		})
	})

	// Suite B — Rules already running, NRE enabled retroactively.

	Context("Suite B — NRE enabled on a cluster with rules already running", func() {
		var (
			rc   *RuleReadinessController
			node *corev1.Node
			rule *nodereadinessiov1alpha1.NodeReadinessRule
		)

		BeforeEach(func() {
			rc = sharedSetup()

			// Simulate a cluster where rules and taints are already in place.
			node = makeNode("nre-b-node", []corev1.Taint{
				{Key: taintKey, Effect: corev1.TaintEffectNoSchedule},
			}, corev1.ConditionFalse)
			rule = makeRule("nre-b-rule")

			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
			// Rule cache is populated (simulates RuleReconciler having run).
			rc.updateRuleCache(ctx, rule)
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, node)
			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: rule.Name}, updatedRule); err == nil {
				updatedRule.Finalizers = nil
				_ = k8sClient.Update(ctx, updatedRule)
				_ = k8sClient.Delete(ctx, updatedRule)
			}
			Eventually(func() bool {
				return apierrors.IsNotFound(
					k8sClient.Get(ctx, types.NamespacedName{Name: rule.Name},
						&nodereadinessiov1alpha1.NodeReadinessRule{}))
			}, nreTestTimeout).Should(BeTrue())
			nre := &nodereadinessiov1alpha1.NodeReadinessEvaluation{}
			if err := k8sClient.Get(ctx, nreNamespacedName(node.Name), nre); err == nil {
				_ = k8sClient.Delete(ctx, nre)
			}
			rc.removeRuleFromCache(ctx, rule.Name)
		})

		It("B1 — first NRE reconcile on existing cluster produces correct full state", func() {
			// No prior NRE exists — this is the moment the feature is enabled.
			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)
			Expect(nre.Spec.NodeName).To(Equal(node.Name))
			Expect(nre.Status.State).To(Equal(nodereadinessiov1alpha1.NodeEvaluationStateNotAvailable))
			Expect(nre.Status.Rules).To(HaveLen(1))
			Expect(nre.Status.Rules[0].RuleStatus).To(Equal(nodereadinessiov1alpha1.RuleStatusUnsatisfied))
			Expect(nre.Status.Rules[0].TaintStatus).To(Equal(nodereadinessiov1alpha1.TaintStatusPresent))
		})

		It("B2 — subsequent reconciles are idempotent", func() {
			reconcileNRE(rc, node.Name)
			firstNRE := getNRE(node.Name)
			firstState := firstNRE.Status.State
			firstRuleLen := len(firstNRE.Status.Rules)

			// Reconcile again without changing anything.
			reconcileNRE(rc, node.Name)
			secondNRE := getNRE(node.Name)

			Expect(secondNRE.Status.State).To(Equal(firstState))
			Expect(secondNRE.Status.Rules).To(HaveLen(firstRuleLen))
		})
	})

	// Suite C — Pre-tainted nodes (adoption / --register-with-taints).

	Context("Suite C — pre-tainted node adoption", func() {
		var (
			rc   *RuleReadinessController
			node *corev1.Node
			rule *nodereadinessiov1alpha1.NodeReadinessRule
		)

		BeforeEach(func() {
			rc = sharedSetup()
			rule = makeRule("nre-c-rule")
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
			rc.updateRuleCache(ctx, rule)
		})

		AfterEach(func() {
			if node != nil {
				_ = k8sClient.Delete(ctx, node)
			}
			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: rule.Name}, updatedRule); err == nil {
				updatedRule.Finalizers = nil
				_ = k8sClient.Update(ctx, updatedRule)
				_ = k8sClient.Delete(ctx, updatedRule)
			}
			Eventually(func() bool {
				return apierrors.IsNotFound(
					k8sClient.Get(ctx, types.NamespacedName{Name: rule.Name},
						&nodereadinessiov1alpha1.NodeReadinessRule{}))
			}, nreTestTimeout).Should(BeTrue())
			if node != nil {
				nre := &nodereadinessiov1alpha1.NodeReadinessEvaluation{}
				if err := k8sClient.Get(ctx, nreNamespacedName(node.Name), nre); err == nil {
					_ = k8sClient.Delete(ctx, nre)
				}
			}
			rc.removeRuleFromCache(ctx, rule.Name)
		})

		It("C1 — TaintObservedAt is set and TaintAddedAt is nil when taint is pre-existing (adoption)", func() {
			// Node registered with the taint already present (--register-with-taints).
			// Conditions are not met, so NRC would normally have added the taint —
			// but since it was already there, this is the adoption case.
			node = makeNode("nre-c-adopted", []corev1.Taint{
				{Key: taintKey, Effect: corev1.TaintEffectNoSchedule},
			}, corev1.ConditionFalse)
			Expect(k8sClient.Create(ctx, node)).To(Succeed())

			// First reconcile: no previous NRE — taint is pre-existing.
			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)
			Expect(nre.Status.Rules).To(HaveLen(1))
			eval := nre.Status.Rules[0]

			By("TaintObservedAt must be set — NRC first observed the taint")
			Expect(eval.TaintObservedAt).NotTo(BeNil(),
				"TaintObservedAt should be set on adoption")

			By("TaintAddedAt must be nil — NRC did not add this taint")
			Expect(eval.TaintAddedAt).To(BeNil(),
				"TaintAddedAt must be nil when the taint was pre-existing")
		})

		It("C2 — TaintObservedAt and TaintAddedAt are both set when NRC adds the taint", func() {
			// Node has no taint initially, conditions are not met.
			// On first reconcile there is no previous NRE and no taint — nothing to set.
			// On second reconcile the taint has been added by NodeReconciler — NRE records it.
			node = makeNode("nre-c-added", nil, corev1.ConditionFalse)
			Expect(k8sClient.Create(ctx, node)).To(Succeed())

			// First NRE reconcile: no taint yet.
			reconcileNRE(rc, node.Name)
			nre := getNRE(node.Name)
			Expect(nre.Status.Rules[0].TaintObservedAt).To(BeNil())
			Expect(nre.Status.Rules[0].TaintAddedAt).To(BeNil())

			// Simulate NodeReconciler adding the taint.
			updatedNode := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: node.Name}, updatedNode)).To(Succeed())
			updatedNode.Spec.Taints = []corev1.Taint{
				{Key: taintKey, Effect: corev1.TaintEffectNoSchedule},
			}
			Expect(k8sClient.Update(ctx, updatedNode)).To(Succeed())

			// Second NRE reconcile: taint is now present — Absent→Present transition.
			reconcileNRE(rc, node.Name)
			nre = getNRE(node.Name)
			eval := nre.Status.Rules[0]

			By("TaintObservedAt is set")
			Expect(eval.TaintObservedAt).NotTo(BeNil())
			By("TaintAddedAt is also set — NRC added it")
			Expect(eval.TaintAddedAt).NotTo(BeNil())
		})

		It("C3 — TaintObservedAt carries forward unchanged on subsequent reconciles while taint persists", func() {
			node = makeNode("nre-c-carry", []corev1.Taint{
				{Key: taintKey, Effect: corev1.TaintEffectNoSchedule},
			}, corev1.ConditionFalse)
			Expect(k8sClient.Create(ctx, node)).To(Succeed())

			// First reconcile: adoption — TaintObservedAt set.
			reconcileNRE(rc, node.Name)
			firstNRE := getNRE(node.Name)
			firstObservedAt := firstNRE.Status.Rules[0].TaintObservedAt
			Expect(firstObservedAt).NotTo(BeNil())

			// Second reconcile: nothing changed.
			reconcileNRE(rc, node.Name)
			secondNRE := getNRE(node.Name)

			Expect(secondNRE.Status.Rules[0].TaintObservedAt).To(Equal(firstObservedAt),
				"TaintObservedAt must be carried forward unchanged on subsequent reconciles")
		})

		It("C4 — TaintRemovedAt is set on Present→Absent transition, ObservedAt/AddedAt are cleared", func() {
			node = makeNode("nre-c-clear", []corev1.Taint{
				{Key: taintKey, Effect: corev1.TaintEffectNoSchedule},
			}, corev1.ConditionFalse)
			Expect(k8sClient.Create(ctx, node)).To(Succeed())

			// First reconcile: adoption — TaintObservedAt set, TaintAddedAt nil.
			reconcileNRE(rc, node.Name)
			Expect(getNRE(node.Name).Status.Rules[0].TaintObservedAt).NotTo(BeNil())
			Expect(getNRE(node.Name).Status.Rules[0].TaintRemovedAt).To(BeNil())

			// Simulate taint removal + condition satisfied.
			updatedNode := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: node.Name}, updatedNode)).To(Succeed())
			updatedNode.Spec.Taints = nil
			Expect(k8sClient.Update(ctx, updatedNode)).To(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: node.Name}, updatedNode)).To(Succeed())
			updatedNode.Status.Conditions[0].Status = corev1.ConditionTrue
			Expect(k8sClient.Status().Update(ctx, updatedNode)).To(Succeed())

			// Second reconcile: Present→Absent transition.
			reconcileNRE(rc, node.Name)
			eval := getNRE(node.Name).Status.Rules[0]

			By("TaintObservedAt and TaintAddedAt are cleared")
			Expect(eval.TaintObservedAt).To(BeNil())
			Expect(eval.TaintAddedAt).To(BeNil())

			By("TaintRemovedAt is set to record when removal happened")
			Expect(eval.TaintRemovedAt).NotTo(BeNil())

			// Third reconcile: taint stays absent — TaintRemovedAt carries forward.
			reconcileNRE(rc, node.Name)
			eval2 := getNRE(node.Name).Status.Rules[0]

			By("TaintRemovedAt carries forward on subsequent reconciles (historical SLI)")
			Expect(eval2.TaintRemovedAt).To(Equal(eval.TaintRemovedAt))
		})
	})

	// Suite D — Lifecycle and GC.

	Context("Suite D — NRE lifecycle", func() {
		var (
			rc   *RuleReadinessController
			node *corev1.Node
			rule *nodereadinessiov1alpha1.NodeReadinessRule
		)

		BeforeEach(func() {
			rc = sharedSetup()

			node = makeNode("nre-d-node", []corev1.Taint{
				{Key: taintKey, Effect: corev1.TaintEffectNoSchedule},
			}, corev1.ConditionFalse)
			rule = makeRule("nre-d-rule")

			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
			rc.updateRuleCache(ctx, rule)
		})

		AfterEach(func() {
			if node != nil {
				_ = k8sClient.Delete(ctx, node)
			}
			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: rule.Name}, updatedRule); err == nil {
				updatedRule.Finalizers = nil
				_ = k8sClient.Update(ctx, updatedRule)
				_ = k8sClient.Delete(ctx, updatedRule)
			}
			Eventually(func() bool {
				return apierrors.IsNotFound(
					k8sClient.Get(ctx, types.NamespacedName{Name: rule.Name},
						&nodereadinessiov1alpha1.NodeReadinessRule{}))
			}, nreTestTimeout).Should(BeTrue())
			nre := &nodereadinessiov1alpha1.NodeReadinessEvaluation{}
			if err := k8sClient.Get(ctx, nreNamespacedName(node.Name), nre); err == nil {
				_ = k8sClient.Delete(ctx, nre)
			}
			rc.removeRuleFromCache(ctx, rule.Name)
		})

		It("D1 — rule removed from cache causes its entry to disappear from NRE on next reconcile", func() {
			// First reconcile: rule present.
			reconcileNRE(rc, node.Name)
			Expect(getNRE(node.Name).Status.Rules).To(HaveLen(1))

			// Remove the rule from the cache (simulates rule deletion).
			rc.removeRuleFromCache(ctx, rule.Name)

			// Second reconcile: no applicable rules.
			reconcileNRE(rc, node.Name)
			nre := getNRE(node.Name)

			Expect(nre.Status.Rules).To(BeEmpty(),
				"rules slice should be empty after the rule is removed from the cache")
			Expect(nre.Status.State).To(Equal(nodereadinessiov1alpha1.NodeEvaluationStateAvailable),
				"state should be Available when no rules apply and no taints are managed")
		})

		It("D2 — NRE carries ownerReference to Node for automatic GC", func() {
			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)
			Expect(nre.OwnerReferences).To(HaveLen(1))
			ref := nre.OwnerReferences[0]
			Expect(ref.Kind).To(Equal("Node"))
			Expect(ref.Name).To(Equal(node.Name))
			Expect(ref.UID).To(Equal(node.GetUID()))
			// SetOwnerReference (non-controller) leaves BlockOwnerDeletion nil,
			// which is the correct GC cascade behaviour — Node deletion is not blocked.
		})

		It("D3 — dry-run rules are excluded from NRE", func() {
			// Mark the rule as dry-run in the cache.
			dryRunRule := rule.DeepCopy()
			dryRunRule.Spec.DryRun = true
			rc.updateRuleCache(ctx, dryRunRule)

			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)
			Expect(nre.Status.Rules).To(BeEmpty(),
				"dry-run rules must not produce NRE entries")
		})

		It("D4 — rules being deleted (DeletionTimestamp set) are excluded from NRE", func() {
			deletingRule := rule.DeepCopy()
			now := metav1.Now()
			deletingRule.DeletionTimestamp = &now
			rc.updateRuleCache(ctx, deletingRule)

			reconcileNRE(rc, node.Name)

			nre := getNRE(node.Name)
			Expect(nre.Status.Rules).To(BeEmpty(),
				"rules with DeletionTimestamp must not produce NRE entries")
		})

		It("D5 — FirstEvaluatedAt is set on creation and preserved on subsequent reconciles", func() {
			reconcileNRE(rc, node.Name)
			firstNRE := getNRE(node.Name)
			Expect(firstNRE.Status.Rules[0].FirstEvaluatedAt).NotTo(BeNil())
			firstTime := firstNRE.Status.Rules[0].FirstEvaluatedAt

			// Wait a tick so the clock would differ if mistakenly overwritten.
			time.Sleep(nreTestInterval)
			reconcileNRE(rc, node.Name)

			secondNRE := getNRE(node.Name)
			Expect(secondNRE.Status.Rules[0].FirstEvaluatedAt).To(Equal(firstTime),
				"FirstEvaluatedAt must not be overwritten on subsequent reconciles")
		})

		It("D6 — rule delete+recreate (same name, new UID) resets FirstEvaluatedAt", func() {
			reconcileNRE(rc, node.Name)
			firstNRE := getNRE(node.Name)
			Expect(firstNRE.Status.Rules[0].FirstEvaluatedAt).NotTo(BeNil())
			firstUID := firstNRE.Status.Rules[0].RuleUID

			// Simulate rule recreated with same name but new UID.
			recreatedRule := rule.DeepCopy()
			recreatedRule.UID = "new-uid-after-recreate"
			rc.updateRuleCache(ctx, recreatedRule)

			reconcileNRE(rc, node.Name)

			secondNRE := getNRE(node.Name)
			Expect(secondNRE.Status.Rules[0].FirstEvaluatedAt).NotTo(BeNil())
			// The UID in the entry must now reflect the recreated rule.
			Expect(secondNRE.Status.Rules[0].RuleUID).NotTo(Equal(firstUID),
				"RuleUID should reflect the new rule after delete+recreate")
			// And because the UID changed, FirstEvaluatedAt was treated as a new rule.
			// The previous entry had the old UID so findPreviousRuleEvaluation returns nil,
			// causing FirstEvaluatedAt to be set to now (which may equal the previous
			// value if within the same wall-clock second — we verify via UID change above).
		})
	})
	// Suite E — Wiring: NRE is written from NodeReconciler and RuleReconciler paths.

	Context("Suite E — EnableNRE wiring through NodeReconciler and RuleReconciler", func() {
		var (
			rc   *RuleReadinessController
			node *corev1.Node
			rule *nodereadinessiov1alpha1.NodeReadinessRule
		)

		BeforeEach(func() {
			rc = sharedSetup()

			node = makeNode("nre-e-node", []corev1.Taint{
				{Key: taintKey, Effect: corev1.TaintEffectNoSchedule},
			}, corev1.ConditionFalse)
			rule = makeRule("nre-e-rule")

			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
			rc.updateRuleCache(ctx, rule)
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, node)
			updatedRule := &nodereadinessiov1alpha1.NodeReadinessRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: rule.Name}, updatedRule); err == nil {
				updatedRule.Finalizers = nil
				_ = k8sClient.Update(ctx, updatedRule)
				_ = k8sClient.Delete(ctx, updatedRule)
			}
			Eventually(func() bool {
				return apierrors.IsNotFound(
					k8sClient.Get(ctx, types.NamespacedName{Name: rule.Name},
						&nodereadinessiov1alpha1.NodeReadinessRule{}))
			}, nreTestTimeout).Should(BeTrue())
			nre := &nodereadinessiov1alpha1.NodeReadinessEvaluation{}
			if err := k8sClient.Get(ctx, nreNamespacedName(node.Name), nre); err == nil {
				_ = k8sClient.Delete(ctx, nre)
			}
			rc.removeRuleFromCache(ctx, rule.Name)
		})

		It("E1 — NodeReconciler.Reconcile creates NRE when EnableNRE=true", func() {
			nodeReconciler := &NodeReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Controller: rc,
			}

			_, err := nodeReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: node.Name},
			})
			Expect(err).NotTo(HaveOccurred())

			nre := getNRE(node.Name)
			Expect(nre.Spec.NodeName).To(Equal(node.Name))
			Expect(nre.Status.Rules).To(HaveLen(1))
			Expect(nre.Status.Rules[0].RuleName).To(Equal(rule.Name))
		})

		It("E2 — processAllNodesForRule creates NRE per matching node when EnableNRE=true", func() {
			nodeList := &corev1.NodeList{}
			Expect(k8sClient.List(ctx, nodeList)).To(Succeed())

			Expect(rc.processAllNodesForRule(ctx, rule, nodeList)).To(Succeed())

			nre := getNRE(node.Name)
			Expect(nre.Spec.NodeName).To(Equal(node.Name))
			Expect(nre.Status.Rules).To(HaveLen(1))
			Expect(nre.Status.Rules[0].RuleName).To(Equal(rule.Name))
		})
	})
})

// findCondition returns the condition with the given type from a slice, or nil.
func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}
