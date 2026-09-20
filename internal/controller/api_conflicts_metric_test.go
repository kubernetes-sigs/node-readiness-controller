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
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	nodereadinessiov1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
	"sigs.k8s.io/node-readiness-controller/internal/metrics"
)

// apiConflictsValue reads the current value of APIConflicts{rule, operation}.
func apiConflictsValue(rule string, op metrics.ConflictOperation) float64 {
	return counterValue(metrics.APIConflicts.WithLabelValues(rule, string(op)))
}

// failuresValue reads the current value of Failures{rule, reason}.
func failuresValue(rule string, reason metrics.FailureReason) float64 {
	return counterValue(metrics.Failures.WithLabelValues(rule, string(reason)))
}

var _ = Describe("node_readiness_api_conflicts_total", func() {
	var (
		ctx        context.Context
		testScheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		testScheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(testScheme)).To(Succeed())
		Expect(nodereadinessiov1alpha1.AddToScheme(testScheme)).To(Succeed())
	})

	It("records an add_taint conflict and retries", func() {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "api-conflict-add-node", Labels: map[string]string{"role": "worker"}},
		}
		rule := &nodereadinessiov1alpha1.NodeReadinessRule{
			ObjectMeta: metav1.ObjectMeta{Name: "api-conflict-add-rule"},
			Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
				Conditions:      []nodereadinessiov1alpha1.ConditionRequirement{{Type: "Ready", RequiredStatus: corev1.ConditionTrue}},
				Taint:           corev1.Taint{Key: "readiness.k8s.io/api-conflict-add", Effect: corev1.TaintEffectNoSchedule},
				NodeSelector:    metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
				EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
			},
		}

		var patchCount atomic.Int32
		fc := fakeclient.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(node, rule).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*corev1.Node); ok && patchCount.Add(1) == 1 {
						// Simulate another writer changing the node between the Get and the Patch.
						current := &corev1.Node{}
						Expect(c.Get(ctx, types.NamespacedName{Name: obj.GetName()}, current)).To(Succeed())
						current.Spec.Taints = append(current.Spec.Taints, corev1.Taint{
							Key: "other-controller/taint", Effect: corev1.TaintEffectNoSchedule,
						})
						Expect(c.Update(ctx, current)).To(Succeed())
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).
			Build()

		controller := &RuleReadinessController{
			Client:        fc,
			Scheme:        testScheme,
			clientset:     fake.NewSimpleClientset(),
			ruleCache:     make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
			EventRecorder: events.NewFakeRecorder(10),
		}

		Expect(fc.Get(ctx, types.NamespacedName{Name: node.Name}, node)).To(Succeed())
		before := apiConflictsValue(rule.Name, metrics.ConflictOperationAddTaint)

		added, err := controller.addTaintBySpec(ctx, node, rule)
		Expect(err).NotTo(HaveOccurred())
		Expect(added).To(BeTrue())
		Expect(patchCount.Load()).To(BeNumerically(">=", 2), "the first patch conflicts, so it should retry")

		Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationAddTaint)).
			To(BeNumerically("==", before+1))
	})

	It("records a finalizer_add conflict and retries", func() {
		rule := &nodereadinessiov1alpha1.NodeReadinessRule{
			ObjectMeta: metav1.ObjectMeta{Name: "api-conflict-finalizer-rule"},
			Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
				Conditions:      []nodereadinessiov1alpha1.ConditionRequirement{{Type: "Ready", RequiredStatus: corev1.ConditionTrue}},
				Taint:           corev1.Taint{Key: "readiness.k8s.io/api-conflict-finalizer", Effect: corev1.TaintEffectNoSchedule},
				NodeSelector:    metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
				EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
			},
		}

		var patchCount atomic.Int32
		fc := fakeclient.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(rule).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*nodereadinessiov1alpha1.NodeReadinessRule); ok && patchCount.Add(1) == 1 {
						current := &nodereadinessiov1alpha1.NodeReadinessRule{}
						Expect(c.Get(ctx, types.NamespacedName{Name: obj.GetName()}, current)).To(Succeed())
						if current.Labels == nil {
							current.Labels = map[string]string{}
						}
						current.Labels["bumped-by"] = "competing-writer"
						Expect(c.Update(ctx, current)).To(Succeed())
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).
			Build()

		reconciler := &RuleReconciler{Client: fc, Scheme: testScheme}

		Expect(fc.Get(ctx, types.NamespacedName{Name: rule.Name}, rule)).To(Succeed())
		before := apiConflictsValue(rule.Name, metrics.ConflictOperationFinalizerAdd)

		added, err := reconciler.ensureFinalizer(ctx, rule, finalizerName)
		Expect(err).NotTo(HaveOccurred())
		Expect(added).To(BeTrue())
		Expect(patchCount.Load()).To(BeNumerically(">=", 2), "the first patch conflicts, so it should retry")

		Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationFinalizerAdd)).
			To(BeNumerically("==", before+1))
	})

	It("records a finalizer_remove conflict during delete and retries", func() {
		deletionTS := metav1.Now()
		rule := &nodereadinessiov1alpha1.NodeReadinessRule{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "api-conflict-finalizer-remove-rule",
				Finalizers:        []string{finalizerName},
				DeletionTimestamp: &deletionTS,
			},
			Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
				Conditions:      []nodereadinessiov1alpha1.ConditionRequirement{{Type: "Ready", RequiredStatus: corev1.ConditionTrue}},
				Taint:           corev1.Taint{Key: "readiness.k8s.io/api-conflict-finalizer-remove", Effect: corev1.TaintEffectNoSchedule},
				NodeSelector:    metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
				EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
			},
		}

		var patchCount atomic.Int32
		fc := fakeclient.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(rule).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*nodereadinessiov1alpha1.NodeReadinessRule); ok && patchCount.Add(1) == 1 {
						current := &nodereadinessiov1alpha1.NodeReadinessRule{}
						Expect(c.Get(ctx, types.NamespacedName{Name: obj.GetName()}, current)).To(Succeed())
						if current.Labels == nil {
							current.Labels = map[string]string{}
						}
						current.Labels["bumped-by"] = "competing-writer"
						Expect(c.Update(ctx, current)).To(Succeed())
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).
			Build()

		controller := &RuleReadinessController{
			Client:        fc,
			Scheme:        testScheme,
			clientset:     fake.NewSimpleClientset(),
			ruleCache:     make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
			EventRecorder: events.NewFakeRecorder(10),
		}
		reconciler := &RuleReconciler{Client: fc, Scheme: testScheme, Controller: controller}

		fetched := &nodereadinessiov1alpha1.NodeReadinessRule{}
		Expect(fc.Get(ctx, types.NamespacedName{Name: rule.Name}, fetched)).To(Succeed())
		controller.updateRuleCache(ctx, fetched)

		before := apiConflictsValue(rule.Name, metrics.ConflictOperationFinalizerRemove)

		result, err := reconciler.reconcileDelete(ctx, fetched, &corev1.NodeList{})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero(), "a successful delete should not requeue")
		Expect(patchCount.Load()).To(BeNumerically(">=", 2), "the first patch conflicts, so it should retry")

		remaining := &nodereadinessiov1alpha1.NodeReadinessRule{}
		err = fc.Get(ctx, types.NamespacedName{Name: rule.Name}, remaining)
		if err == nil {
			Expect(remaining.Finalizers).NotTo(ContainElement(finalizerName))
		} else {
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the rule may be deleted once its last finalizer is gone")
		}

		Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationFinalizerRemove)).
			To(BeNumerically("==", before+1))
	})

	It("records a remove_taint conflict and retries", func() {
		rule := &nodereadinessiov1alpha1.NodeReadinessRule{
			ObjectMeta: metav1.ObjectMeta{Name: "api-conflict-remove-rule"},
			Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
				Conditions:      []nodereadinessiov1alpha1.ConditionRequirement{{Type: "Ready", RequiredStatus: corev1.ConditionTrue}},
				Taint:           corev1.Taint{Key: "readiness.k8s.io/api-conflict-remove", Effect: corev1.TaintEffectNoSchedule},
				NodeSelector:    metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
				EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
			},
		}

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "api-conflict-remove-node", Labels: map[string]string{"role": "worker"}},
			Spec: corev1.NodeSpec{Taints: []corev1.Taint{
				{Key: "readiness.k8s.io/api-conflict-remove", Effect: corev1.TaintEffectNoSchedule},
				{Key: "other-controller/taint", Effect: corev1.TaintEffectNoSchedule},
			}},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: "Ready", Status: corev1.ConditionTrue}}},
		}

		var patchCount atomic.Int32
		fc := fakeclient.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(node, rule).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*corev1.Node); ok && patchCount.Add(1) == 1 {
						current := &corev1.Node{}
						Expect(c.Get(ctx, types.NamespacedName{Name: obj.GetName()}, current)).To(Succeed())
						current.Spec.Taints = append(current.Spec.Taints, corev1.Taint{
							Key: "third-controller/taint", Effect: corev1.TaintEffectNoSchedule,
						})
						Expect(c.Update(ctx, current)).To(Succeed())
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).
			Build()

		controller := &RuleReadinessController{
			Client:        fc,
			Scheme:        testScheme,
			clientset:     fake.NewSimpleClientset(),
			ruleCache:     make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
			EventRecorder: events.NewFakeRecorder(10),
		}

		Expect(fc.Get(ctx, types.NamespacedName{Name: node.Name}, node)).To(Succeed())
		before := apiConflictsValue(rule.Name, metrics.ConflictOperationRemoveTaint)

		err := controller.removeTaintBySpec(ctx, node, rule.Spec.Taint, rule.Name)
		Expect(err).NotTo(HaveOccurred())
		Expect(patchCount.Load()).To(BeNumerically(">=", 2), "the first patch conflicts, so it should retry")

		updated := &corev1.Node{}
		Expect(fc.Get(ctx, types.NamespacedName{Name: node.Name}, updated)).To(Succeed())
		Expect(controller.hasTaintBySpec(updated, rule.Spec.Taint)).To(BeFalse(), "the taint should be gone")

		Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationRemoveTaint)).
			To(BeNumerically("==", before+1))
	})

	It("records a mark_bootstrap_completed conflict and retries", func() {
		rule := &nodereadinessiov1alpha1.NodeReadinessRule{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-conflict-bootstrap-rule",
				UID:  types.UID("9a9a9a9a-9a9a-9a9a-9a9a-9a9a9a9a9a9a"),
			},
			Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
				Conditions:      []nodereadinessiov1alpha1.ConditionRequirement{{Type: "Ready", RequiredStatus: corev1.ConditionTrue}},
				Taint:           corev1.Taint{Key: "readiness.k8s.io/api-conflict-bootstrap", Effect: corev1.TaintEffectNoSchedule},
				NodeSelector:    metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
				EnforcementMode: nodereadinessiov1alpha1.EnforcementModeBootstrapOnly,
			},
		}

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "api-conflict-bootstrap-node", Labels: map[string]string{"role": "worker"}},
			Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: "Ready", Status: corev1.ConditionTrue}}},
		}

		var patchCount atomic.Int32
		fc := fakeclient.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(node, rule).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*corev1.Node); ok && patchCount.Add(1) == 1 {
						current := &corev1.Node{}
						Expect(c.Get(ctx, types.NamespacedName{Name: obj.GetName()}, current)).To(Succeed())
						if current.Labels == nil {
							current.Labels = map[string]string{}
						}
						current.Labels["bumped-by"] = "competing-writer"
						Expect(c.Update(ctx, current)).To(Succeed())
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).
			Build()

		controller := &RuleReadinessController{
			Client:        fc,
			Scheme:        testScheme,
			clientset:     fake.NewSimpleClientset(),
			ruleCache:     make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
			EventRecorder: events.NewFakeRecorder(10),
		}

		before := apiConflictsValue(rule.Name, metrics.ConflictOperationMarkBootstrapCompleted)

		controller.markBootstrapCompleted(ctx, node.Name, rule)
		Expect(patchCount.Load()).To(BeNumerically(">=", 2), "the first patch conflicts, so it should retry")

		updated := &corev1.Node{}
		Expect(fc.Get(ctx, types.NamespacedName{Name: node.Name}, updated)).To(Succeed())
		Expect(updated.Annotations).To(HaveKey(bootstrapAnnotationKey(rule.GetUID())),
			"the retry should mark bootstrap complete")

		Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationMarkBootstrapCompleted)).
			To(BeNumerically("==", before+1))
	})

	conflictOnEveryNodePatch := func(patchCount *atomic.Int32) interceptor.Funcs {
		return interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if _, ok := obj.(*corev1.Node); ok {
					n := patchCount.Add(1)
					current := &corev1.Node{}
					Expect(c.Get(ctx, types.NamespacedName{Name: obj.GetName()}, current)).To(Succeed())
					if current.Labels == nil {
						current.Labels = map[string]string{}
					}
					current.Labels["bumped-by"] = fmt.Sprintf("competing-writer-%d", n)
					Expect(c.Update(ctx, current)).To(Succeed())
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}
	}

	It("records AddTaintConflictExhausted when retries run out", func() {
		rule := &nodereadinessiov1alpha1.NodeReadinessRule{
			ObjectMeta: metav1.ObjectMeta{Name: "api-conflict-add-exhaust-rule"},
			Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
				Conditions:      []nodereadinessiov1alpha1.ConditionRequirement{{Type: "Ready", RequiredStatus: corev1.ConditionTrue}},
				Taint:           corev1.Taint{Key: "readiness.k8s.io/api-conflict-add-exhaust", Effect: corev1.TaintEffectNoSchedule},
				NodeSelector:    metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
				EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
			},
		}
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "api-conflict-add-exhaust-node", Labels: map[string]string{"role": "worker"}},
			Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: "Ready", Status: corev1.ConditionFalse}}},
		}

		var patchCount atomic.Int32
		fc := fakeclient.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(node, rule).
			WithInterceptorFuncs(conflictOnEveryNodePatch(&patchCount)).
			Build()

		controller := &RuleReadinessController{
			Client:        fc,
			Scheme:        testScheme,
			clientset:     fake.NewSimpleClientset(),
			ruleCache:     make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
			EventRecorder: events.NewFakeRecorder(10),
		}

		Expect(fc.Get(ctx, types.NamespacedName{Name: node.Name}, node)).To(Succeed())
		beforeExhausted := failuresValue(rule.Name, metrics.FailureReasonAddTaintConflictExhausted)
		beforeAddError := failuresValue(rule.Name, metrics.FailureReasonAddTaintError)
		beforeConflicts := apiConflictsValue(rule.Name, metrics.ConflictOperationAddTaint)

		err := controller.evaluateRuleForNode(ctx, rule, node)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsConflict(err)).To(BeTrue(), "the last conflict should be returned")
		Expect(patchCount.Load()).To(BeNumerically(">=", 5), "all retries should conflict")

		Expect(failuresValue(rule.Name, metrics.FailureReasonAddTaintConflictExhausted)).
			To(BeNumerically("==", beforeExhausted+1),
				"should record AddTaintConflictExhausted")
		Expect(failuresValue(rule.Name, metrics.FailureReasonAddTaintError)).
			To(BeNumerically("==", beforeAddError), "should not record AddTaintError")
		Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationAddTaint)).
			To(BeNumerically("==", beforeConflicts+float64(patchCount.Load())),
				"each conflict should be counted")
	})

	It("records RemoveTaintConflictExhausted when retries run out", func() {
		rule := &nodereadinessiov1alpha1.NodeReadinessRule{
			ObjectMeta: metav1.ObjectMeta{Name: "api-conflict-remove-exhaust-rule"},
			Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
				Conditions:      []nodereadinessiov1alpha1.ConditionRequirement{{Type: "Ready", RequiredStatus: corev1.ConditionTrue}},
				Taint:           corev1.Taint{Key: "readiness.k8s.io/api-conflict-remove-exhaust", Effect: corev1.TaintEffectNoSchedule},
				NodeSelector:    metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
				EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
			},
		}
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "api-conflict-remove-exhaust-node", Labels: map[string]string{"role": "worker"}},
			Spec: corev1.NodeSpec{Taints: []corev1.Taint{
				{Key: "readiness.k8s.io/api-conflict-remove-exhaust", Effect: corev1.TaintEffectNoSchedule},
			}},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: "Ready", Status: corev1.ConditionTrue}}},
		}

		var patchCount atomic.Int32
		fc := fakeclient.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(node, rule).
			WithInterceptorFuncs(conflictOnEveryNodePatch(&patchCount)).
			Build()

		controller := &RuleReadinessController{
			Client:        fc,
			Scheme:        testScheme,
			clientset:     fake.NewSimpleClientset(),
			ruleCache:     make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
			EventRecorder: events.NewFakeRecorder(10),
		}

		Expect(fc.Get(ctx, types.NamespacedName{Name: node.Name}, node)).To(Succeed())
		beforeExhausted := failuresValue(rule.Name, metrics.FailureReasonRemoveTaintConflictExhausted)
		beforeRemoveError := failuresValue(rule.Name, metrics.FailureReasonRemoveTaintError)
		beforeConflicts := apiConflictsValue(rule.Name, metrics.ConflictOperationRemoveTaint)

		err := controller.evaluateRuleForNode(ctx, rule, node)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsConflict(err)).To(BeTrue(), "the last conflict should be returned")
		Expect(patchCount.Load()).To(BeNumerically(">=", 5), "all retries should conflict")

		Expect(failuresValue(rule.Name, metrics.FailureReasonRemoveTaintConflictExhausted)).
			To(BeNumerically("==", beforeExhausted+1),
				"should record RemoveTaintConflictExhausted")
		Expect(failuresValue(rule.Name, metrics.FailureReasonRemoveTaintError)).
			To(BeNumerically("==", beforeRemoveError), "should not record RemoveTaintError")
		Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationRemoveTaint)).
			To(BeNumerically("==", beforeConflicts+float64(patchCount.Load())),
				"each conflict should be counted")
	})

	Context("status patch conflicts use the caller's operation label", func() {
		newRuleWithNode := func(ruleName, nodeName string) (*nodereadinessiov1alpha1.NodeReadinessRule, *corev1.Node) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName, Labels: map[string]string{"role": "worker"}},
				Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: "Ready", Status: corev1.ConditionTrue}}},
			}
			rule := &nodereadinessiov1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: ruleName},
				Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
					Conditions:      []nodereadinessiov1alpha1.ConditionRequirement{{Type: "Ready", RequiredStatus: corev1.ConditionTrue}},
					Taint:           corev1.Taint{Key: "readiness.k8s.io/" + ruleName, Effect: corev1.TaintEffectNoSchedule},
					NodeSelector:    metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
					EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
				},
			}
			return rule, node
		}

		conflictOnFirstStatusPatch := func(ruleName string, patchCount *atomic.Int32) interceptor.Funcs {
			return interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					if r, ok := obj.(*nodereadinessiov1alpha1.NodeReadinessRule); ok && r.Name == ruleName && patchCount.Add(1) == 1 {
						current := &nodereadinessiov1alpha1.NodeReadinessRule{}
						Expect(c.Get(ctx, types.NamespacedName{Name: r.Name}, current)).To(Succeed())
						current.Status.NodeEvaluations = append(current.Status.NodeEvaluations, nodereadinessiov1alpha1.NodeEvaluation{
							NodeName:    "competing-node",
							TaintStatus: nodereadinessiov1alpha1.TaintStatusAbsent,
						})
						Expect(c.Status().Update(ctx, current)).To(Succeed())
					}
					return c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
				},
			}
		}

		conflictOnEveryStatusPatch := func(ruleName string, patchCount *atomic.Int32) interceptor.Funcs {
			return interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					if r, ok := obj.(*nodereadinessiov1alpha1.NodeReadinessRule); ok && r.Name == ruleName {
						n := patchCount.Add(1)
						competing := &nodereadinessiov1alpha1.NodeReadinessRule{}
						Expect(c.Get(ctx, types.NamespacedName{Name: r.Name}, competing)).To(Succeed())
						competing.Status.NodeEvaluations = append(competing.Status.NodeEvaluations, nodereadinessiov1alpha1.NodeEvaluation{
							NodeName:    fmt.Sprintf("competing-node-%d", n),
							TaintStatus: nodereadinessiov1alpha1.TaintStatusAbsent,
						})
						Expect(c.Status().Update(ctx, competing)).To(Succeed())
					}
					return c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
				},
			}
		}

		It("uses rule_status_node_write for NodeReconciler status updates", func() {
			rule, node := newRuleWithNode("api-conflict-node-write-rule", "api-conflict-node-write-node")

			var patchCount atomic.Int32
			fc := fakeclient.NewClientBuilder().
				WithScheme(testScheme).
				WithObjects(node, rule).
				WithStatusSubresource(rule).
				WithInterceptorFuncs(conflictOnFirstStatusPatch(rule.Name, &patchCount)).
				Build()

			controller := &RuleReadinessController{
				Client:        fc,
				Scheme:        testScheme,
				clientset:     fake.NewSimpleClientset(),
				ruleCache:     map[string]*nodereadinessiov1alpha1.NodeReadinessRule{rule.Name: rule},
				EventRecorder: events.NewFakeRecorder(10),
			}

			beforeNodeWrite := apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusNodeWrite)
			beforeSweep := apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusRuleSweep)

			Expect(controller.processNodeAgainstAllRules(ctx, node)).To(Succeed())
			Expect(patchCount.Load()).To(BeNumerically(">=", 2), "the first patch conflicts, so it should retry")

			Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusNodeWrite)).
				To(BeNumerically("==", beforeNodeWrite+1), "should use rule_status_node_write")
			Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusRuleSweep)).
				To(BeNumerically("==", beforeSweep), "should not use rule_status_rule_sweep")
		})

		It("uses rule_status_rule_sweep for RuleReconciler status updates", func() {
			rule, node := newRuleWithNode("api-conflict-sweep-rule", "api-conflict-sweep-node")

			var patchCount atomic.Int32
			fc := fakeclient.NewClientBuilder().
				WithScheme(testScheme).
				WithObjects(node, rule).
				WithStatusSubresource(rule).
				WithInterceptorFuncs(conflictOnFirstStatusPatch(rule.Name, &patchCount)).
				Build()

			controller := &RuleReadinessController{
				Client:        fc,
				Scheme:        testScheme,
				clientset:     fake.NewSimpleClientset(),
				ruleCache:     make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
				EventRecorder: events.NewFakeRecorder(10),
			}
			controller.updateRuleCache(ctx, rule)

			nodeList := &corev1.NodeList{Items: []corev1.Node{*node}}
			delta, err := controller.processAllNodesForRule(ctx, rule, nodeList)
			Expect(err).NotTo(HaveOccurred())

			beforeSweep := apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusRuleSweep)
			beforeNodeWrite := apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusNodeWrite)

			Expect(controller.updateRuleStatus(ctx, rule, delta)).To(Succeed())
			Expect(patchCount.Load()).To(BeNumerically(">=", 2), "the first patch conflicts, so it should retry")

			Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusRuleSweep)).
				To(BeNumerically("==", beforeSweep+1), "should use rule_status_rule_sweep")
			Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusNodeWrite)).
				To(BeNumerically("==", beforeNodeWrite), "should not use rule_status_node_write")
		})

		It("uses rule_status_rule_sweep when cleaning up deleted nodes", func() {
			rule, node := newRuleWithNode("api-conflict-cleanup-rule", "api-conflict-cleanup-node")
			rule.Status.NodeEvaluations = []nodereadinessiov1alpha1.NodeEvaluation{
				{NodeName: "deleted-node", TaintStatus: nodereadinessiov1alpha1.TaintStatusAbsent},
			}

			var patchCount atomic.Int32
			fc := fakeclient.NewClientBuilder().
				WithScheme(testScheme).
				WithObjects(node, rule).
				WithStatusSubresource(rule).
				WithInterceptorFuncs(conflictOnFirstStatusPatch(rule.Name, &patchCount)).
				Build()

			controller := &RuleReadinessController{
				Client:        fc,
				Scheme:        testScheme,
				clientset:     fake.NewSimpleClientset(),
				ruleCache:     make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
				EventRecorder: events.NewFakeRecorder(10),
			}

			beforeSweep := apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusRuleSweep)
			beforeNodeWrite := apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusNodeWrite)

			nodeList := &corev1.NodeList{Items: []corev1.Node{*node}}
			Expect(controller.cleanupDeletedNodes(ctx, rule, nodeList)).To(Succeed())
			Expect(patchCount.Load()).To(BeNumerically(">=", 2), "the first patch conflicts, so it should retry")

			Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusRuleSweep)).
				To(BeNumerically("==", beforeSweep+1), "should use rule_status_rule_sweep")
			Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusNodeWrite)).
				To(BeNumerically("==", beforeNodeWrite), "should not use rule_status_node_write")
		})

		It("records RuleStatusRuleSweepConflictExhausted when retries run out", func() {
			rule, node := newRuleWithNode("api-conflict-exhaust-rule", "api-conflict-exhaust-node")

			var patchCount atomic.Int32
			fc := fakeclient.NewClientBuilder().
				WithScheme(testScheme).
				WithObjects(node, rule).
				WithStatusSubresource(rule).
				WithInterceptorFuncs(conflictOnEveryStatusPatch(rule.Name, &patchCount)).
				Build()

			controller := &RuleReadinessController{
				Client:        fc,
				Scheme:        testScheme,
				clientset:     fake.NewSimpleClientset(),
				ruleCache:     make(map[string]*nodereadinessiov1alpha1.NodeReadinessRule),
				EventRecorder: events.NewFakeRecorder(10),
			}
			controller.updateRuleCache(ctx, rule)

			nodeList := &corev1.NodeList{Items: []corev1.Node{*node}}
			delta, err := controller.processAllNodesForRule(ctx, rule, nodeList)
			Expect(err).NotTo(HaveOccurred())

			beforeSweepExhausted := failuresValue(rule.Name, metrics.FailureReasonRuleStatusRuleSweepConflictExhausted)
			beforeNodeExhausted := failuresValue(rule.Name, metrics.FailureReasonStatusPatchConflictExhausted)
			beforePatchError := failuresValue(rule.Name, metrics.FailureReasonStatusPatchError)
			beforeSweepConflicts := apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusRuleSweep)

			err = controller.updateRuleStatus(ctx, rule, delta)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsConflict(err)).To(BeTrue(), "the last conflict should be returned")
			Expect(patchCount.Load()).To(BeNumerically(">=", 5), "all retries should conflict")

			Expect(failuresValue(rule.Name, metrics.FailureReasonRuleStatusRuleSweepConflictExhausted)).
				To(BeNumerically("==", beforeSweepExhausted+1),
					"should record RuleStatusRuleSweepConflictExhausted")
			Expect(failuresValue(rule.Name, metrics.FailureReasonStatusPatchConflictExhausted)).
				To(BeNumerically("==", beforeNodeExhausted),
					"should not record StatusPatchConflictExhausted")
			Expect(failuresValue(rule.Name, metrics.FailureReasonStatusPatchError)).
				To(BeNumerically("==", beforePatchError), "should not record StatusPatchError")
			Expect(apiConflictsValue(rule.Name, metrics.ConflictOperationRuleStatusRuleSweep)).
				To(BeNumerically("==", beforeSweepConflicts+float64(patchCount.Load())),
					"each conflict should be counted")
		})
	})
})
