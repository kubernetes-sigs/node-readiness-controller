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
	"encoding/json"
	"maps"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

//nolint:godot
const (
	// bootstrapAnnotationPrefix is the common prefix for all bootstrap completion
	// annotations on a Node. The suffix is the rule's metadata.uid (RFC 4122 UUID,
	// ~36 chars), which is immutable for the object's lifetime and globally unique.
	//
	// Full key format: readiness.k8s.io/bootstrap-completed-<ruleUID>
	// Value format:    {"rule-name":"<ruleName>"}   (for human readability)
	bootstrapAnnotationPrefix = "readiness.k8s.io/bootstrap-completed-"
)

// bootstrapAnnotationKey returns the annotation key for a rule's bootstrap
// completion state, using the rule's UID as the suffix.
func bootstrapAnnotationKey(uid types.UID) string {
	return bootstrapAnnotationPrefix + string(uid)
}

// bootstrapAnnotationValue returns the JSON-encoded value to store in the
// bootstrap annotation. It includes the rule name for human readability.
func bootstrapAnnotationValue(ruleName string) string {
	v := struct {
		RuleName string `json:"rule-name"`
	}{RuleName: ruleName}
	b, err := json.Marshal(v)
	if err != nil {
		return `{"rule-name":""}` // should never happen
	}
	return string(b)
}

// legacyBootstrapAnnotationKey returns the old-format annotation key used
// before the UID migration: readiness.k8s.io/bootstrap-completed-<ruleName>.
func legacyBootstrapAnnotationKey(ruleName string) string {
	return bootstrapAnnotationPrefix + ruleName
}

// conditionsEqual checks if two condition slices are equal.
func conditionsEqual(a, b []corev1.NodeCondition) bool {
	if len(a) != len(b) {
		return false
	}

	// Create map for quick lookup
	aMap := make(map[corev1.NodeConditionType]corev1.ConditionStatus)
	for _, cond := range a {
		aMap[cond.Type] = cond.Status
	}

	for _, cond := range b {
		if status, exists := aMap[cond.Type]; !exists || status != cond.Status {
			return false
		}
	}

	return true
}

// taintsEqual checks if two taint slices are equal.
func taintsEqual(a, b []corev1.Taint) bool {
	if len(a) != len(b) {
		return false
	}

	// Create map for quick lookup
	aMap := make(map[string]corev1.Taint)
	for _, taint := range a {
		key := taint.Key + string(taint.Effect)
		aMap[key] = taint
	}

	for _, taint := range b {
		key := taint.Key + string(taint.Effect)
		oldTaint, exists := aMap[key]
		if !exists || oldTaint.Value != taint.Value {
			return false
		}
	}

	return true
}

// filters nodeEvaluations and failedNodes to keep only existing nodes.
func filterStatusForExistingNodes(
	existingNodes map[string]bool,
	nodeEvaluations []readinessv1alpha1.NodeEvaluation,
	failedNodes []readinessv1alpha1.NodeFailure,
) ([]readinessv1alpha1.NodeEvaluation, []readinessv1alpha1.NodeFailure) {
	filteredEvaluations := make([]readinessv1alpha1.NodeEvaluation, 0, len(nodeEvaluations))
	for _, evaluation := range nodeEvaluations {
		if existingNodes[evaluation.NodeName] {
			filteredEvaluations = append(filteredEvaluations, evaluation)
		}
	}

	filteredFailedNodes := make([]readinessv1alpha1.NodeFailure, 0, len(failedNodes))
	for _, failure := range failedNodes {
		if existingNodes[failure.NodeName] {
			filteredFailedNodes = append(filteredFailedNodes, failure)
		}
	}

	return filteredEvaluations, filteredFailedNodes
}

// labelsEqual checks if two label maps are equal.
func labelsEqual(a, b map[string]string) bool {
	return maps.Equal(a, b)
}

// nodeStatusDelta captures the per-node NodeEvaluation/NodeFailure changes a single
// processAllNodesForRule sweep actually produced, keyed by node name. It intentionally does not
// carry AppliedNodes/ObservedGeneration/DryRunResults: those fields only ever have one writer
// (RuleReconciler), so a plain overwrite of them is safe.
//
// A nil value in failures means "clear any failure recorded for this node" (the node evaluated
// successfully). evaluations only ever holds entries for nodes that were freshly (re-)evaluated
// this sweep; a failed evaluation leaves the node's prior NodeEvaluation untouched.
type nodeStatusDelta struct {
	evaluations map[string]readinessv1alpha1.NodeEvaluation
	failures    map[string]*readinessv1alpha1.NodeFailure
}

// applyNodeStatusDelta merges delta into rule's NodeEvaluations/FailedNodes, replacing only the
// entries for nodes present in delta and leaving every other node's entry untouched.
//
// This is the crux of fixing the lost-update bug described in #341: a naive full-slice
// replacement of NodeEvaluations/FailedNodes (computed from a nodeList snapshot taken at the
// start of a RuleReconciler sweep) would silently discard any per-node status update written
// concurrently by NodeReconciler for a node this particular sweep didn't touch. Merging by node
// name instead means each writer only ever overwrites the entries it just recomputed.
func applyNodeStatusDelta(rule *readinessv1alpha1.NodeReadinessRule, delta nodeStatusDelta) {
	if len(delta.evaluations) > 0 {
		merged := make([]readinessv1alpha1.NodeEvaluation, 0, len(rule.Status.NodeEvaluations)+len(delta.evaluations))
		for _, eval := range rule.Status.NodeEvaluations {
			if _, changed := delta.evaluations[eval.NodeName]; !changed {
				merged = append(merged, eval)
			}
		}
		for _, eval := range delta.evaluations {
			merged = append(merged, eval)
		}
		sort.Slice(merged, func(i, j int) bool { return merged[i].NodeName < merged[j].NodeName })
		rule.Status.NodeEvaluations = merged
	}

	if len(delta.failures) > 0 {
		merged := make([]readinessv1alpha1.NodeFailure, 0, len(rule.Status.FailedNodes)+len(delta.failures))
		for _, failure := range rule.Status.FailedNodes {
			if _, changed := delta.failures[failure.NodeName]; !changed {
				merged = append(merged, failure)
			}
		}
		for _, failure := range delta.failures {
			if failure != nil {
				merged = append(merged, *failure)
			}
		}
		sort.Slice(merged, func(i, j int) bool { return merged[i].NodeName < merged[j].NodeName })
		rule.Status.FailedNodes = merged
	}
}
