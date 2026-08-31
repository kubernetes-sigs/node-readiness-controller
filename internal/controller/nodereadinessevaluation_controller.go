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
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessevaluations,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessevaluations/status,verbs=get;update;patch

// updateNREForNode writes (or updates) the NodeReadinessEvaluation for the given
// node using the current rule cache. It is called as a side-effect from both
// NodeReconciler.Reconcile (after processing the node against all rules) and
// RuleReconciler.Reconcile (after processing all nodes for the changed rule).
func (r *RuleReadinessController) updateNREForNode(ctx context.Context, node *corev1.Node) {
	log := ctrl.LoggerFrom(ctx)

	nre, err := r.ensureNRE(ctx, node)
	if err != nil {
		log.Error(err, "Failed to ensure NRE for node", "node", node.Name)
		return
	}

	// Evaluate all applicable rules from the shared cache.
	applicableRules := r.getApplicableRulesForNode(ctx, node)
	log.V(4).Info("Evaluating rules for NRE", "node", node.Name, "ruleCount", len(applicableRules))

	// Snapshot the previous rules slice for timestamp carry-forward BEFORE
	// clearing it. buildRuleEvaluation reads from this snapshot via nre.Status.Rules,
	// so it must not be cleared until after all evaluations are complete.
	prevRules := make([]readinessv1alpha1.RuleEvaluation, len(nre.Status.Rules))
	copy(prevRules, nre.Status.Rules)

	patch := client.MergeFrom(nre.DeepCopy())

	// Rebuild the full rules slice from scratch on every reconcile.
	// listType=atomic means the controller owns the whole slice.
	newRules := make([]readinessv1alpha1.RuleEvaluation, 0, len(applicableRules))
	for _, rule := range applicableRules {
		if !rule.DeletionTimestamp.IsZero() || rule.Spec.DryRun {
			continue
		}
		ruleEval := r.buildRuleEvaluation(ctx, node, rule, prevRules)
		newRules = append(newRules, ruleEval)
	}
	nre.Status.Rules = newRules

	recomputeNREStatus(&nre.Status)

	if err := r.Status().Patch(ctx, nre, patch); err != nil {
		log.Error(err, "Failed to patch NRE status", "node", node.Name)
		return
	}

	log.V(4).Info("Updated NRE", "node", node.Name, "rules", len(nre.Status.Rules), "state", nre.Status.State)
}

// ensureNRE fetches the NRE for the node, creating it (with ownerReference) if
// it does not exist yet. Returns the current object ready for status patching.
func (r *RuleReadinessController) ensureNRE(ctx context.Context, node *corev1.Node) (*readinessv1alpha1.NodeReadinessEvaluation, error) {
	nre := &readinessv1alpha1.NodeReadinessEvaluation{}
	err := r.Get(ctx, client.ObjectKey{Name: node.Name}, nre)

	switch {
	case apierrors.IsNotFound(err):
		nre = &readinessv1alpha1.NodeReadinessEvaluation{
			ObjectMeta: metav1.ObjectMeta{
				Name: node.Name,
			},
			Spec: readinessv1alpha1.NodeReadinessEvaluationSpec{
				NodeName: node.Name,
			},
		}
		if ownerErr := controllerutil.SetOwnerReference(node, nre, r.Scheme); ownerErr != nil {
			return nil, fmt.Errorf("failed to set owner reference on NRE %s: %w", node.Name, ownerErr)
		}
		if createErr := r.Create(ctx, nre); createErr != nil {
			if !apierrors.IsAlreadyExists(createErr) {
				return nil, fmt.Errorf("failed to create NRE %s: %w", node.Name, createErr)
			}
			// A concurrent reconcile won the race — re-fetch.
			if getErr := r.Get(ctx, client.ObjectKey{Name: node.Name}, nre); getErr != nil {
				return nil, fmt.Errorf("failed to get NRE %s after AlreadyExists: %w", node.Name, getErr)
			}
		}
		ctrl.LoggerFrom(ctx).V(4).Info("Created NodeReadinessEvaluation", "nre", node.Name)

	case err != nil:
		return nil, fmt.Errorf("failed to get NRE %s: %w", node.Name, err)
	}

	return nre, nil
}

// buildRuleEvaluation evaluates a single rule against the node and constructs
// the RuleEvaluation entry, preserving SLI timestamps from prevRules (the
// snapshot of the previous status.rules slice taken before the rebuild started).
func (r *RuleReadinessController) buildRuleEvaluation(
	ctx context.Context,
	node *corev1.Node,
	rule *readinessv1alpha1.NodeReadinessRule,
	prevRules []readinessv1alpha1.RuleEvaluation,
) readinessv1alpha1.RuleEvaluation {
	log := ctrl.LoggerFrom(ctx)
	now := metav1.Now()

	// Evaluate all conditions.
	allConditionsSatisfied := true
	conditionResults := make([]readinessv1alpha1.ConditionEvaluationResult, 0, len(rule.Spec.Conditions))
	for _, condReq := range rule.Spec.Conditions {
		effectiveStatus, conditionFound := r.getConditionStatus(node, condReq.Type, condReq.GetDefaultStatus())
		satisfied := effectiveStatus == condReq.RequiredStatus
		if !satisfied {
			allConditionsSatisfied = false
		}
		observedStatus := effectiveStatus
		if !conditionFound {
			observedStatus = corev1.ConditionUnknown
		}
		conditionResults = append(conditionResults, readinessv1alpha1.ConditionEvaluationResult{
			Type:           condReq.Type,
			CurrentStatus:  observedStatus,
			RequiredStatus: condReq.RequiredStatus,
			DefaultStatus:  condReq.GetDefaultStatus(),
		})
	}

	ruleStatus := readinessv1alpha1.RuleStatusSatisfied
	if !allConditionsSatisfied {
		ruleStatus = readinessv1alpha1.RuleStatusUnsatisfied
	}

	taintPresent := r.hasTaintBySpec(node, rule.Spec.Taint)
	taintStatus := readinessv1alpha1.TaintStatusAbsent
	if taintPresent {
		taintStatus = readinessv1alpha1.TaintStatusPresent
	}

	reason, message := buildRuleEvalReasonMessage(ruleStatus, taintStatus, rule.Spec.Taint.Key, conditionResults)

	log.V(4).Info("Rule evaluation for NRE",
		"node", node.Name, "rule", rule.Name,
		"ruleStatus", ruleStatus, "taintStatus", taintStatus)

	// Carry forward timestamps from the previous evaluation entry if present.
	prev := findPreviousRuleEvaluation(prevRules, rule.Name)

	eval := readinessv1alpha1.RuleEvaluation{
		RuleName:            rule.Name,
		RuleUID:             rule.GetUID(),
		RuleStatus:          ruleStatus,
		TaintStatus:         taintStatus,
		TaintKey:            rule.Spec.Taint.Key,
		TaintEffect:         rule.Spec.Taint.Effect,
		Reason:              reason,
		Message:             message,
		ReadinessConditions: conditionResults,
		LastEvaluationTime:  now,
	}

	// FirstEvaluatedAt: set once, carried forward on subsequent evaluations.
	if prev != nil && prev.RuleUID == rule.GetUID() {
		eval.FirstEvaluatedAt = prev.FirstEvaluatedAt
	} else {
		// No previous entry, or the rule was deleted and recreated (UID changed).
		eval.FirstEvaluatedAt = &now
	}

	// Taint timestamp transitions — four cases covering every state combination.
	// TaintRemovedAt is carried forward once set so the SLI is always queryable,
	// not just on the exact reconcile cycle where removal happened.
	if prev != nil && prev.RuleUID == rule.GetUID() {
		prevTaintPresent := prev.TaintStatus == readinessv1alpha1.TaintStatusPresent
		switch {
		case taintPresent && prevTaintPresent:
			// State unchanged: taint still active — carry forward all timestamps.
			eval.TaintObservedAt = prev.TaintObservedAt
			eval.TaintAddedAt = prev.TaintAddedAt
		case taintPresent && !prevTaintPresent:
			// Transition: Absent → Present — NRC just added the taint.
			eval.TaintObservedAt = &now
			eval.TaintAddedAt = &now
		case !taintPresent && prevTaintPresent:
			// Transition: Present → Absent — NRC just removed the taint.
			eval.TaintRemovedAt = &now
		default:
			// State unchanged: taint still absent — carry forward historical removal time.
			eval.TaintRemovedAt = prev.TaintRemovedAt
		}
	} else if taintPresent {
		// First evaluation for this rule (or rule recreated) and taint is already
		// present — NRC is adopting a pre-existing taint.
		eval.TaintObservedAt = &now
		// TaintAddedAt stays nil — NRC did not add this taint.
	}

	return eval
}

// buildRuleEvalReasonMessage derives the Reason and Message for a RuleEvaluation
// based on the rule outcome and the per-condition results. Both fields are
// human-readable diagnostic aids — Reason is machine-readable (no spaces),
// Message is the operator-facing explanation.
func buildRuleEvalReasonMessage(
	ruleStatus readinessv1alpha1.RuleStatus,
	taintStatus readinessv1alpha1.TaintStatus,
	taintKey string,
	conditions []readinessv1alpha1.ConditionEvaluationResult,
) (reason, message string) {
	switch {
	case ruleStatus == readinessv1alpha1.RuleStatusSatisfied && taintStatus == readinessv1alpha1.TaintStatusAbsent:
		return "NodeReady", fmt.Sprintf("All conditions satisfied and taint %q is absent.", taintKey)

	case ruleStatus == readinessv1alpha1.RuleStatusSatisfied && taintStatus == readinessv1alpha1.TaintStatusPresent:
		return "TaintPendingRemoval", fmt.Sprintf("All conditions satisfied; taint %q is still present and pending removal.", taintKey)

	case ruleStatus == readinessv1alpha1.RuleStatusUnsatisfied && taintStatus == readinessv1alpha1.TaintStatusPresent:
		var unsatisfied []string
		for _, c := range conditions {
			if c.CurrentStatus != c.RequiredStatus {
				unsatisfied = append(unsatisfied, fmt.Sprintf("%s (current=%s, required=%s)", c.Type, c.CurrentStatus, c.RequiredStatus))
			}
		}
		return "ConditionsNotSatisfied", fmt.Sprintf(
			"Taint %q is active. Unsatisfied condition(s): %s.",
			taintKey, strings.Join(unsatisfied, "; "),
		)

	default:
		// Unmatched + Absent: conditions not met but taint is also gone (e.g. manually removed).
		var unsatisfied []string
		for _, c := range conditions {
			if c.CurrentStatus != c.RequiredStatus {
				unsatisfied = append(unsatisfied, fmt.Sprintf("%s (current=%s, required=%s)", c.Type, c.CurrentStatus, c.RequiredStatus))
			}
		}
		return "ConditionsNotSatisfied", fmt.Sprintf(
			"Taint %q is absent but conditions are not satisfied: %s.",
			taintKey, strings.Join(unsatisfied, "; "),
		)
	}
}

// findPreviousRuleEvaluation returns the existing RuleEvaluation entry for the
// given rule name in the previous rules snapshot, or nil if not found.
func findPreviousRuleEvaluation(prevRules []readinessv1alpha1.RuleEvaluation, ruleName string) *readinessv1alpha1.RuleEvaluation {
	for i := range prevRules {
		if prevRules[i].RuleName == ruleName {
			return &prevRules[i]
		}
	}
	return nil
}

// recomputeNREStatus derives status.State and status.Conditions
// from the current rules slice. Called after every full rebuild so all top-level
// fields stay consistent.
func recomputeNREStatus(status *readinessv1alpha1.NodeReadinessEvaluationStatus) {
	var activeTaints int32

	for _, r := range status.Rules {
		if r.TaintStatus == readinessv1alpha1.TaintStatusPresent {
			activeTaints++
		}
	}

	if activeTaints > 0 {
		status.State = readinessv1alpha1.NodeEvaluationStateNotAvailable
	} else {
		status.State = readinessv1alpha1.NodeEvaluationStateAvailable
	}

	// "Evaluated" — did the controller successfully complete a full evaluation pass?
	// Evaluation is pure in-memory (no I/O), so this is always true after Reconcile returns.
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               "Evaluated",
		Status:             metav1.ConditionTrue,
		Reason:             "EvaluationSuccessful",
		Message:            "All applicable rules were evaluated successfully.",
		LastTransitionTime: metav1.Now(),
	})

	// "Available" — does the node satisfy all rules with zero active taints?
	availableCond := metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionTrue,
		Reason:             "NodeAvailable",
		Message:            "Node satisfies all rules, has zero active taints, and is available for scheduling.",
		LastTransitionTime: metav1.Now(),
	}
	if activeTaints > 0 {
		var blockingRules []string
		for _, r := range status.Rules {
			if r.TaintStatus == readinessv1alpha1.TaintStatusPresent {
				blockingRules = append(blockingRules, r.RuleName)
			}
		}
		availableCond.Status = metav1.ConditionFalse
		availableCond.Reason = "TaintsActive"
		availableCond.Message = fmt.Sprintf("Node is blocked by %d active taint(s) from rule(s): %s.",
			activeTaints, strings.Join(blockingRules, ", "))
	}
	meta.SetStatusCondition(&status.Conditions, availableCond)
}
