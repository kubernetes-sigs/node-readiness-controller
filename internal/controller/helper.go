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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// nodeSelectorChanged checks if nodeSelector has changed.
func nodeSelectorChanged(current, previous metav1.LabelSelector) bool {
	// Compare matchLabels
	if !stringMapEqual(current.MatchLabels, previous.MatchLabels) {
		return true
	}

	// Compare matchExpressions
	if len(current.MatchExpressions) != len(previous.MatchExpressions) {
		return true
	}

	// Create maps for comparison
	currentExprs := make(map[string]metav1.LabelSelectorRequirement)
	for _, expr := range current.MatchExpressions {
		key := fmt.Sprintf("%s-%s-%v", expr.Key, expr.Operator, expr.Values)
		currentExprs[key] = expr
	}

	previousExprs := make(map[string]metav1.LabelSelectorRequirement)
	for _, expr := range previous.MatchExpressions {
		key := fmt.Sprintf("%s-%s-%v", expr.Key, expr.Operator, expr.Values)
		previousExprs[key] = expr
	}

	for key := range currentExprs {
		if _, exists := previousExprs[key]; !exists {
			return true
		}
	}

	return false
}

// stringMapEqual checks if two string maps are equal.
func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if b[k] != v {
			return false
		}
	}

	return true
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

// labelsEqual checks if two label maps are equal.
func labelsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if b[k] != v {
			return false
		}
	}

	return true
}
