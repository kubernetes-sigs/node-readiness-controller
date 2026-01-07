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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

// NodeReadinessRuleWebhook validates NodeReadinessRule resources.
type NodeReadinessRuleWebhook struct {
	client.Client
}

// NewNodeReadinessRuleWebhook creates a new webhook.
func NewNodeReadinessRuleWebhook(c client.Client) *NodeReadinessRuleWebhook {
	return &NodeReadinessRuleWebhook{
		Client: c,
	}
}

// +kubebuilder:webhook:path=/validate-nodereadiness-io-v1alpha1-nodereadinessrule,mutating=false,failurePolicy=fail,sideEffects=None,groups=readiness.node.x-k8s.io,resources=nodereadinessrules,verbs=create;update,versions=v1alpha1,name=vnodereadinessrule.kb.io,admissionReviewVersions=v1

// validateNodeReadinessRule performs validation logic.
func (w *NodeReadinessRuleWebhook) validateNodeReadinessRule(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, isUpdate bool) field.ErrorList {
	var allErrs field.ErrorList

	// Validate basic fields
	allErrs = append(allErrs, w.validateSpec(rule.Spec)...)

	// Check for conflicting rules (same taint key)
	allErrs = append(allErrs, w.validateTaintConflicts(ctx, rule, isUpdate)...)

	return allErrs
}

// validateSpec validates the spec fields.
func (w *NodeReadinessRuleWebhook) validateSpec(spec readinessv1alpha1.NodeReadinessRuleSpec) field.ErrorList {
	var allErrs field.ErrorList
	specField := field.NewPath("spec")

	// Validate conditions
	if len(spec.Conditions) == 0 {
		allErrs = append(allErrs, field.Required(specField.Child("conditions"), "at least one condition is required"))
	}

	for i, condition := range spec.Conditions {
		condField := specField.Child("conditions").Index(i)
		if condition.Type == "" {
			allErrs = append(allErrs, field.Required(condField.Child("type"), "condition type cannot be empty"))
		}
		if condition.RequiredStatus == "" {
			allErrs = append(allErrs, field.Required(condField.Child("requiredStatus"), "required status cannot be empty"))
		}
	}

	// Validate nodeSelector
	selector, err := metav1.LabelSelectorAsSelector(&spec.NodeSelector)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(specField.Child("nodeSelector"), spec.NodeSelector, err.Error()))
	}

	// Validate that the nodeSelector isn't empty.
	if selector != nil && selector.Empty() {
		allErrs = append(allErrs, field.Required(specField.Child("nodeSelector"), "nodeSelector must not be empty"))
	}

	// Validate taint
	taintField := specField.Child("taint")
	if spec.Taint.Key == "" {
		allErrs = append(allErrs, field.Required(taintField.Child("key"), "taint key cannot be empty"))
	}
	if spec.Taint.Effect == "" {
		allErrs = append(allErrs, field.Required(taintField.Child("effect"), "taint effect cannot be empty"))
	}

	// Validate enforcement mode
	if spec.EnforcementMode != readinessv1alpha1.EnforcementModeBootstrapOnly &&
		spec.EnforcementMode != readinessv1alpha1.EnforcementModeContinuous {
		allErrs = append(allErrs, field.Invalid(
			specField.Child("enforcementMode"),
			spec.EnforcementMode,
			"must be 'bootstrap-only' or 'continuous'",
		))
	}

	return allErrs
}

// validateTaintConflicts checks for conflicting rules with the same taint key.
func (w *NodeReadinessRuleWebhook) validateTaintConflicts(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, isUpdate bool) field.ErrorList {
	var allErrs field.ErrorList

	// List all existing rules
	ruleList := &readinessv1alpha1.NodeReadinessRuleList{}
	if err := w.List(ctx, ruleList); err != nil {
		// If we can't list rules, allow the operation but log the issue
		ctrl.Log.Error(err, "Failed to list rules for conflict validation")
		return allErrs
	}

	taintField := field.NewPath("spec", "taint", "key")

	for _, existingRule := range ruleList.Items {
		// Skip self when updating
		if isUpdate && existingRule.Name == rule.Name {
			continue
		}

		// Check for same taint key and effect
		if existingRule.Spec.Taint.Key == rule.Spec.Taint.Key &&
			existingRule.Spec.Taint.Effect == rule.Spec.Taint.Effect {
			// Check if node selectors overlap
			if w.nodSelectorsOverlap(rule.Spec.NodeSelector, existingRule.Spec.NodeSelector) {
				allErrs = append(allErrs, field.Invalid(
					taintField,
					rule.Spec.Taint.Key,
					fmt.Sprintf("conflicts with existing rule '%s' - same taint key '%s' and effect '%s' with overlapping node selectors",
						existingRule.Name, rule.Spec.Taint.Key, rule.Spec.Taint.Effect),
				))
			}
		}
	}

	return allErrs
}

// nodeSelectorsOverlap checks if two node selectors overlap.
func (w *NodeReadinessRuleWebhook) nodSelectorsOverlap(selector1, selector2 metav1.LabelSelector) bool {
	// Convert to selectors
	sel1, err1 := metav1.LabelSelectorAsSelector(&selector1)
	sel2, err2 := metav1.LabelSelectorAsSelector(&selector2)

	if err1 != nil || err2 != nil {
		// If we can't parse selectors, assume they overlap for safety
		return true
	}

	// Simple heuristic: if selectors are identical, they definitely overlap
	// For more complex overlap detection, we'd need to analyze the label requirements
	return sel1.String() == sel2.String()
}

// SetupWithManager sets up the webhook with the manager.
func (w *NodeReadinessRuleWebhook) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&readinessv1alpha1.NodeReadinessRule{}).
		WithValidator(w).
		Complete()
}

// Implement the admission.CustomValidator interface.
var _ webhook.CustomValidator = &NodeReadinessRuleWebhook{}

func (w *NodeReadinessRuleWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	rule, ok := obj.(*readinessv1alpha1.NodeReadinessRule)
	if !ok {
		return nil, fmt.Errorf("expected NodeReadinessRule, got %T", obj)
	}

	if allErrs := w.validateNodeReadinessRule(ctx, rule, false); len(allErrs) > 0 {
		return nil, fmt.Errorf("validation failed: %v", allErrs)
	}
	return nil, nil
}

func (w *NodeReadinessRuleWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	rule, ok := newObj.(*readinessv1alpha1.NodeReadinessRule)
	if !ok {
		return nil, fmt.Errorf("expected NodeReadinessRule, got %T", newObj)
	}

	if allErrs := w.validateNodeReadinessRule(ctx, rule, true); len(allErrs) > 0 {
		return nil, fmt.Errorf("validation failed: %v", allErrs)
	}
	return nil, nil
}

func (w *NodeReadinessRuleWebhook) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// No validation needed for delete operations
	return nil, nil
}
