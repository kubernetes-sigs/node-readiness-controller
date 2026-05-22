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

	corev1 "k8s.io/api/core/v1"
)

const (
	// bootstrapStateAnnotation is the node annotation key that stores a JSON map
	// of rule names to their bootstrap completion status.
	// Using a single annotation with a structured payload avoids Kubernetes'
	// 63-character name-segment limit on annotation keys and keeps all rule
	// state visible in one place — the same pattern kubectl uses for
	// kubectl.kubernetes.io/last-applied-configuration.
	bootstrapStateAnnotation = "readiness.k8s.io/bootstrap-state"
)

// getBootstrapState deserializes the bootstrap state map from the node annotation.
// It returns an empty map if the annotation is absent or unparseable.
func getBootstrapState(node *corev1.Node) map[string]bool {
	state := make(map[string]bool)
	if node.Annotations == nil {
		return state
	}
	raw, ok := node.Annotations[bootstrapStateAnnotation]
	if !ok || raw == "" {
		return state
	}
	// Best-effort parse; ignore malformed values.
	_ = json.Unmarshal([]byte(raw), &state)
	return state
}

// serializeBootstrapState serializes the bootstrap state map to a JSON string
// for storage in the node annotation. Errors are silently ignored and return "{}".
func serializeBootstrapState(state map[string]bool) string {
	b, err := json.Marshal(state)
	if err != nil {
		return "{}"
	}
	return string(b)
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
