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
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// NodeReadinessChanged is a predicate that filters out Node Update events where
// conditions, taints, or labels have not changed (such as routine kubelet heartbeats).
type NodeReadinessChanged struct {
	logger logr.Logger
}

// NewNodeReadinessChanged returns a new NodeReadinessChanged predicate.
func NewNodeReadinessChanged(logger logr.Logger) NodeReadinessChanged {
	return NodeReadinessChanged{
		logger: logger,
	}
}

// Create admits all Node creation events so new nodes are evaluated against rules.
func (p NodeReadinessChanged) Create(event.CreateEvent) bool {
	return true
}

// Delete admits all Node deletion events so removed nodes are cleaned up from rule status.
func (p NodeReadinessChanged) Delete(event.DeleteEvent) bool {
	return true
}

// Update admits events only when conditions, taints, or labels change.
func (p NodeReadinessChanged) Update(e event.UpdateEvent) bool {
	oldNode, okOld := e.ObjectOld.(*corev1.Node)
	newNode, okNew := e.ObjectNew.(*corev1.Node)
	if !okOld || !okNew || oldNode == nil || newNode == nil {
		p.logger.Error(
			nil,
			"Update event has invalid or missing node objects",
			"event", e,
		)
		return false
	}

	conditionsChanged := !conditionsEqual(
		oldNode.Status.Conditions,
		newNode.Status.Conditions,
	)
	taintsChanged := !taintsEqual(
		oldNode.Spec.Taints,
		newNode.Spec.Taints,
	)
	labelsChanged := !labelsEqual(
		oldNode.Labels,
		newNode.Labels,
	)

	shouldReconcile := conditionsChanged || taintsChanged || labelsChanged
	if shouldReconcile {
		p.logger.V(4).Info(
			"NodeReadinessChanged predicate matched node update",
			"node", newNode.Name,
			"conditionsChanged", conditionsChanged,
			"taintsChanged", taintsChanged,
			"labelsChanged", labelsChanged,
		)
	}

	return shouldReconcile
}

// Generic ignores generic events as they do not represent Node readiness changes.
func (p NodeReadinessChanged) Generic(event.GenericEvent) bool {
	return false
}
