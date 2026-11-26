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
	"strings"

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

// taintIdentity is the equality-relevant identity of a taint for
// taintsEqual. TimeAdded is intentionally excluded: the predicate in
// node_controller.go only cares whether the taint set changed semantically.
type taintIdentity struct {
	Key    string
	Value  string
	Effect corev1.TaintEffect
}

// taintsEqual checks if two taint slices are equal as multisets.
func taintsEqual(a, b []corev1.Taint) bool {
	if len(a) != len(b) {
		return false
	}

	counts := make(map[taintIdentity]int, len(a))
	for _, taint := range a {
		k := taintIdentity{Key: taint.Key, Value: taint.Value, Effect: taint.Effect}
		counts[k]++
	}

	for _, taint := range b {
		k := taintIdentity{Key: taint.Key, Value: taint.Value, Effect: taint.Effect}
		c, ok := counts[k]
		if !ok || c == 0 {
			return false
		}
		counts[k] = c - 1
	}

	return true
}

// labelsEqual checks if two label maps are equal.
func labelsEqual(a, b map[string]string) bool {
	return maps.Equal(a, b)
}

// nodeStatusRelevantChange reports whether a node update can change what a rule's status
// reports for it: its conditions, taints, labels (rule selection) or the bootstrap
// completion annotations.
func nodeStatusRelevantChange(oldNode, newNode *corev1.Node) bool {
	return !conditionsEqual(oldNode.Status.Conditions, newNode.Status.Conditions) ||
		!taintsEqual(oldNode.Spec.Taints, newNode.Spec.Taints) ||
		!labelsEqual(oldNode.Labels, newNode.Labels) ||
		!maps.Equal(bootstrapAnnotations(oldNode), bootstrapAnnotations(newNode))
}

// bootstrapAnnotations returns the bootstrap completion annotations of a node.
func bootstrapAnnotations(node *corev1.Node) map[string]string {
	var out map[string]string
	for k, v := range node.Annotations {
		if strings.HasPrefix(k, bootstrapAnnotationPrefix) {
			if out == nil {
				out = make(map[string]string)
			}
			out[k] = v
		}
	}
	return out
}

// conditionOutcome is the evaluation of a single rule condition against a node.
type conditionOutcome struct {
	result    readinessv1alpha1.ConditionEvaluationResult
	effective corev1.ConditionStatus // observed status, or the condition's default if absent
	satisfied bool
}

// evaluateRuleConditions evaluates every condition of rule against node without side effects
// (no metrics, no logging). It returns the per-condition outcomes and whether the rule's
// conditionPolicy is met (allOf: every condition satisfied; anyOf: at least one).
func (r *RuleReadinessController) evaluateRuleConditions(node *corev1.Node, rule *readinessv1alpha1.NodeReadinessRule) ([]conditionOutcome, bool) {
	outcomes := make([]conditionOutcome, 0, len(rule.Spec.Conditions))
	allSatisfied := true
	anySatisfied := false

	for _, condReq := range rule.Spec.Conditions {
		effective, found := r.getConditionStatus(node, condReq.Type, condReq.GetDefaultStatus())
		satisfied := effective == condReq.RequiredStatus
		if satisfied {
			anySatisfied = true
		} else {
			allSatisfied = false
		}

		// observed is the condition status without applying the default fallback.
		observed := effective
		if !found {
			observed = corev1.ConditionUnknown
		}

		outcomes = append(outcomes, conditionOutcome{
			result: readinessv1alpha1.ConditionEvaluationResult{
				Type:           condReq.Type,
				CurrentStatus:  observed,
				RequiredStatus: condReq.RequiredStatus,
				DefaultStatus:  condReq.GetDefaultStatus(),
			},
			effective: effective,
			satisfied: satisfied,
		})
	}

	if rule.Spec.GetConditionPolicy() == readinessv1alpha1.ConditionPolicyAnyOf {
		return outcomes, anySatisfied
	}
	return outcomes, allSatisfied
}
