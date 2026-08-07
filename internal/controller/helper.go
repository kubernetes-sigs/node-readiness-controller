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

// labelsEqual checks if two label maps hold the same keys with the same values.
func labelsEqual(a, b map[string]string) bool {
	return maps.Equal(a, b)
}

// removeString removes all occurrences of str from slice.
func removeString(slice []string, str string) []string {
	var result []string
	for _, item := range slice {
		if item != str {
			result = append(result, item)
		}
	}
	return result
}

func isNodeInEvaluations(evals []readinessv1alpha1.NodeEvaluation, nodeName string) bool {
	for _, e := range evals {
		if e.NodeName == nodeName {
			return true
		}
	}
	return false
}

func removeNodeEvaluation(evals []readinessv1alpha1.NodeEvaluation, nodeName string) []readinessv1alpha1.NodeEvaluation {
	var res []readinessv1alpha1.NodeEvaluation
	for _, e := range evals {
		if e.NodeName != nodeName {
			res = append(res, e)
		}
	}
	return res
}

func isNodeInApplied(applied []string, nodeName string) bool {
	for _, n := range applied {
		if n == nodeName {
			return true
		}
	}
	return false
}

func isNodeInFailed(failures []readinessv1alpha1.NodeFailure, nodeName string) bool {
	for _, f := range failures {
		if f.NodeName == nodeName {
			return true
		}
	}
	return false
}

func removeNodeFailure(failures []readinessv1alpha1.NodeFailure, nodeName string) []readinessv1alpha1.NodeFailure {
	var res []readinessv1alpha1.NodeFailure
	for _, f := range failures {
		if f.NodeName != nodeName {
			res = append(res, f)
		}
	}
	return res
}
