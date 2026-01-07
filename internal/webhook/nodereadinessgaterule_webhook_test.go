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

package webhook

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

func TestWebhook(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook Suite")
}

var _ = Describe("NodeReadinessRule Validation Webhook", func() {
	var (
		ctx     context.Context
		webhook *NodeReadinessRuleWebhook
		scheme  *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(readinessv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		webhook = NewNodeReadinessRuleWebhook(fakeClient)
	})

	Context("Spec Validation", func() {
		It("should validate required fields", func() {
			rule := &readinessv1alpha1.NodeReadinessRule{
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					// Missing conditions, taint, and enforcement mode
				},
			}

			allErrs := webhook.validateSpec(rule.Spec)
			Expect(allErrs).To(HaveLen(5)) // conditions, nodeSelector, taint.key, taint.effect, enforcementMode

			// Check specific errors
			var foundErrors []string
			for _, err := range allErrs {
				foundErrors = append(foundErrors, err.Field)
			}

			Expect(foundErrors).To(ContainElement("spec.conditions"))
			Expect(foundErrors).To(ContainElement("spec.nodeSelector"))
			Expect(foundErrors).To(ContainElement("spec.taint.key"))
			Expect(foundErrors).To(ContainElement("spec.taint.effect"))
			Expect(foundErrors).To(ContainElement("spec.enforcementMode"))
		})

		It("should validate condition requirements", func() {
			rule := &readinessv1alpha1.NodeReadinessRule{
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{
							// Missing type and requiredStatus
						},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
					Taint: corev1.Taint{
						Key:    "test-key",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			allErrs := webhook.validateSpec(rule.Spec)
			Expect(allErrs).To(HaveLen(2)) // condition.type and condition.requiredStatus

			var foundErrors []string
			for _, err := range allErrs {
				foundErrors = append(foundErrors, err.Field)
			}

			Expect(foundErrors).To(ContainElement("spec.conditions[0].type"))
			Expect(foundErrors).To(ContainElement("spec.conditions[0].requiredStatus"))
		})

		Context("Validate nodeSelector", func() {
			It("nodeSelector should be set", func() {
				rule := &readinessv1alpha1.NodeReadinessRule{
					Spec: readinessv1alpha1.NodeReadinessRuleSpec{
						Conditions: []readinessv1alpha1.ConditionRequirement{
							{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
						},
						Taint: corev1.Taint{
							Key:    "test-key",
							Effect: corev1.TaintEffectNoSchedule,
						},
						EnforcementMode: readinessv1alpha1.EnforcementModeContinuous,
					},
				}

				allErrs := webhook.validateSpec(rule.Spec)
				Expect(allErrs).To(HaveLen(1))
				Expect(allErrs[0].Field).To(Equal("spec.nodeSelector"))
				Expect(allErrs[0].Type).To(Equal(field.ErrorTypeRequired))
			})
			It("with invalid nodeSelector", func() {
				rule := &readinessv1alpha1.NodeReadinessRule{
					Spec: readinessv1alpha1.NodeReadinessRuleSpec{
						Conditions: []readinessv1alpha1.ConditionRequirement{
							{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
						},
						NodeSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"-123-worker": "machine",
							},
						},
						Taint: corev1.Taint{
							Key:    "test-key",
							Effect: corev1.TaintEffectNoSchedule,
						},
						EnforcementMode: readinessv1alpha1.EnforcementModeContinuous,
					},
				}

				allErrs := webhook.validateSpec(rule.Spec)
				Expect(allErrs).To(HaveLen(1))
				Expect(allErrs[0].Field).To(Equal("spec.nodeSelector"))
				Expect(allErrs[0].Type).To(Equal(field.ErrorTypeInvalid))
			})
		})

		It("should validate enforcement mode values", func() {
			rule := &readinessv1alpha1.NodeReadinessRule{
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
					Taint: corev1.Taint{
						Key:    "test-key",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: "invalid-mode",
				},
			}

			allErrs := webhook.validateSpec(rule.Spec)
			Expect(allErrs).To(HaveLen(1))
			Expect(allErrs[0].Field).To(Equal("spec.enforcementMode"))
			Expect(allErrs[0].Type).To(Equal(field.ErrorTypeInvalid))
		})

		It("should pass validation for valid spec", func() {
			rule := &readinessv1alpha1.NodeReadinessRule{
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
						{Type: "NetworkReady", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "test-key",
						Effect: corev1.TaintEffectNoSchedule,
						Value:  "pending",
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeBootstrapOnly,
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
				},
			}

			allErrs := webhook.validateSpec(rule.Spec)
			Expect(allErrs).To(BeEmpty())
		})
	})

	Context("Taint Conflict Detection", func() {
		It("should detect conflicting rules with same taint key", func() {
			// Create existing rule
			existingRule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "existing-rule"},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "conflict-key",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			// Create client with existing rule
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(existingRule).
				Build()
			webhook = NewNodeReadinessRuleWebhook(fakeClient)

			// New rule with same taint key
			newRule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "new-rule"},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "NetworkReady", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "conflict-key",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			allErrs := webhook.validateTaintConflicts(ctx, newRule, false)
			Expect(allErrs).To(HaveLen(1))
			Expect(allErrs[0].Field).To(Equal("spec.taint.key"))
			Expect(allErrs[0].Type).To(Equal(field.ErrorTypeInvalid))
			Expect(allErrs[0].Detail).To(ContainSubstring("conflicts with existing rule"))
		})

		It("should allow same taint key with different effects", func() {
			// Create existing rule
			existingRule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "existing-rule"},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "same-key",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			// Create client with existing rule
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(existingRule).
				Build()
			webhook = NewNodeReadinessRuleWebhook(fakeClient)

			// New rule with same key but different effect
			newRule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "new-rule"},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "NetworkReady", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "same-key",
						Effect: corev1.TaintEffectNoExecute, // Different effect
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			allErrs := webhook.validateTaintConflicts(ctx, newRule, false)
			Expect(allErrs).To(BeEmpty()) // No conflicts - different effects
		})

		It("should allow updates to the same rule", func() {
			// Create existing rule
			existingRule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "update-rule"},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					Taint: corev1.Taint{
						Key:    "update-key",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			// Create client with existing rule
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(existingRule).
				Build()
			webhook = NewNodeReadinessRuleWebhook(fakeClient)

			// Update same rule (should not conflict with itself)
			updatedRule := existingRule.DeepCopy()
			updatedRule.Spec.Conditions = []readinessv1alpha1.ConditionRequirement{
				{Type: "NetworkReady", RequiredStatus: corev1.ConditionTrue}, // Changed condition
			}

			allErrs := webhook.validateTaintConflicts(ctx, updatedRule, true) // isUpdate = true
			Expect(allErrs).To(BeEmpty())                                     // No conflicts - updating same rule
		})
	})

	Context("Node Selector Overlap Detection", func() {
		It("should detect overlapping nil selectors", func() {
			overlaps := webhook.nodSelectorsOverlap(metav1.LabelSelector{}, metav1.LabelSelector{})
			Expect(overlaps).To(BeTrue()) // Both nil = both match all nodes
		})

		It("should not overlap when one selector is nil", func() {
			selector := metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-role.kubernetes.io/worker": "",
				},
			}

			overlaps := webhook.nodSelectorsOverlap(metav1.LabelSelector{}, selector)
			Expect(overlaps).To(BeFalse())

			overlaps = webhook.nodSelectorsOverlap(selector, metav1.LabelSelector{})
			Expect(overlaps).To(BeFalse())
		})

		It("should detect identical selectors as overlapping", func() {
			selector1 := metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-role.kubernetes.io/worker": "",
				},
			}

			selector2 := metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-role.kubernetes.io/worker": "",
				},
			}

			overlaps := webhook.nodSelectorsOverlap(selector1, selector2)
			Expect(overlaps).To(BeTrue()) // Identical selectors overlap
		})

		It("should not detect different selectors as overlapping", func() {
			selector1 := metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-role.kubernetes.io/worker": "",
				},
			}

			selector2 := metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-role.kubernetes.io/control-plane": "",
				},
			}

			overlaps := webhook.nodSelectorsOverlap(selector1, selector2)
			Expect(overlaps).To(BeFalse()) // Different selectors don't overlap (simple heuristic)
		})
	})

	Context("CustomValidator Interface", func() {
		It("should validate create operations", func() {
			rule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "create-test"},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
					Taint: corev1.Taint{
						Key:    "create-test-key",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			warnings, err := webhook.ValidateCreate(ctx, rule)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("should reject invalid create operations", func() {
			rule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid-create"},
				Spec:       readinessv1alpha1.NodeReadinessRuleSpec{
					// Missing required fields
				},
			}

			warnings, err := webhook.ValidateCreate(ctx, rule)
			Expect(err).To(HaveOccurred())
			Expect(warnings).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("validation failed"))
		})

		It("should validate update operations", func() {
			oldRule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "update-test"},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
					Taint: corev1.Taint{
						Key:    "update-test-key",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			newRule := oldRule.DeepCopy()
			newRule.Spec.EnforcementMode = readinessv1alpha1.EnforcementModeContinuous

			warnings, err := webhook.ValidateUpdate(ctx, oldRule, newRule)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("should allow delete operations", func() {
			rule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-test"},
			}

			warnings, err := webhook.ValidateDelete(ctx, rule)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("should reject wrong object types", func() {
			wrongObject := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "not-a-rule"},
			}

			warnings, err := webhook.ValidateCreate(ctx, wrongObject)
			Expect(err).To(HaveOccurred())
			Expect(warnings).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("expected NodeReadinessRule"))
		})
	})

	Context("Full Validation Integration", func() {
		It("should perform comprehensive validation", func() {
			// Create existing rule to test conflict detection
			existingRule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "existing-comprehensive"},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "Ready", RequiredStatus: corev1.ConditionTrue},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
					Taint: corev1.Taint{
						Key:    "comprehensive-key",
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			// Create client with existing rule
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(existingRule).
				Build()
			webhook = NewNodeReadinessRuleWebhook(fakeClient)

			// Test valid rule (no conflicts)
			validRule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "valid-comprehensive"},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "NetworkReady", RequiredStatus: corev1.ConditionTrue},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
					Taint: corev1.Taint{
						Key:    "different-key", // No conflict
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeContinuous,
				},
			}

			allErrs := webhook.validateNodeReadinessRule(ctx, validRule, false)
			Expect(allErrs).To(BeEmpty())

			// Test conflicting rule
			conflictingRule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "conflicting-comprehensive"},
				Spec: readinessv1alpha1.NodeReadinessRuleSpec{
					Conditions: []readinessv1alpha1.ConditionRequirement{
						{Type: "StorageReady", RequiredStatus: corev1.ConditionTrue},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
					Taint: corev1.Taint{
						Key:    "comprehensive-key", // Conflicts with existing
						Effect: corev1.TaintEffectNoSchedule,
					},
					EnforcementMode: readinessv1alpha1.EnforcementModeBootstrapOnly,
				},
			}

			allErrs = webhook.validateNodeReadinessRule(ctx, conflictingRule, false)
			Expect(allErrs).To(HaveLen(1))
			Expect(allErrs[0].Field).To(Equal("spec.taint.key"))

			// Test invalid spec
			invalidRule := &readinessv1alpha1.NodeReadinessRule{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid-comprehensive"},
				Spec:       readinessv1alpha1.NodeReadinessRuleSpec{
					// Missing required fields
				},
			}

			allErrs = webhook.validateNodeReadinessRule(ctx, invalidRule, false)
			Expect(allErrs).To(HaveLen(5)) // Multiple validation failures
		})
	})
})
