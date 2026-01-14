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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

// NodeReconciler reconciles a Node object.
type NodeReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Controller *RuleReadinessController
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("node").
		For(&corev1.Node{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				log := ctrl.LoggerFrom(ctx)
				n, ok := e.Object.(*corev1.Node)
				if !ok {
					log.V(4).Info("Expected Node", "type", fmt.Sprintf("%T", e.Object))
					return false
				}
				log.V(4).Info("NodeReconciler processing node create event", "node", n.GetName())
				return true
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				log := ctrl.LoggerFrom(ctx)
				oldNode := e.ObjectOld.(*corev1.Node)
				newNode := e.ObjectNew.(*corev1.Node)

				conditionsChanged := !conditionsEqual(oldNode.Status.Conditions, newNode.Status.Conditions)
				taintsChanged := !taintsEqual(oldNode.Spec.Taints, newNode.Spec.Taints)
				labelsChanged := !labelsEqual(oldNode.Labels, newNode.Labels)

				shouldReconcile := conditionsChanged || taintsChanged || labelsChanged

				if shouldReconcile {
					log.V(4).Info("NodeReconciler processing node update event",
						"node", newNode.Name,
						"conditionsChanged", conditionsChanged,
						"taintsChanged", taintsChanged,
						"labelsChanged", labelsChanged)
				}

				return shouldReconcile
			},
		})).
		Complete(r)
}

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=nodes/finalizers,verbs=update

// NodeReconciler handles node changes

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling node", "node", req.Name)

	// Fetch the node
	node := &corev1.Node{}
	if err := r.Get(ctx, req.NamespacedName, node); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Process node against all applicable rules
	r.Controller.processNodeAgainstAllRules(ctx, node)

	return ctrl.Result{}, nil
}

// processNodeAgainstAllRules processes a single node against all applicable rules.
func (r *RuleReadinessController) processNodeAgainstAllRules(ctx context.Context, node *corev1.Node) {
	log := ctrl.LoggerFrom(ctx)

	// Get all known (cached) applicable rules for this node
	applicableRules := r.getApplicableRulesForNode(ctx, node)
	log.Info("Processing node against rules", "node", node.Name, "ruleCount", len(applicableRules))

	for _, rule := range applicableRules {
		log.V(4).Info("Processing rule from cache",
			"node", node.Name,
			"rule", rule.Name,
			"resourceVersion", rule.ResourceVersion,
			"generation", rule.Generation)

		if !rule.DeletionTimestamp.IsZero() {
			log.V(4).Info("Skipping rule being deleted",
				"node", node.Name,
				"rule", rule.Name)
			continue
		}

		// Skip if bootstrap-only and already completed
		if r.isBootstrapCompleted(node.Name, rule.Name) && rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly {
			log.Info("Skipping bootstrap-only rule - already completed",
				"node", node.Name, "rule", rule.Name)
			continue
		}

		// Skip if dry run or global dry run
		if rule.Spec.DryRun || r.globalDryRun {
			log.Info("Skipping rule - dry run mode",
				"node", node.Name, "rule", rule.Name)
			continue
		}

		log.Info("Evaluating rule for node",
			"node", node.Name,
			"rule", rule.Name,
			"ruleResourceVersion", rule.ResourceVersion)

		if err := r.evaluateRuleForNode(ctx, rule, node); err != nil {
			log.Error(err, "Failed to evaluate rule for node",
				"node", node.Name, "rule", rule.Name)
			// Continue with other rules even if one fails
			r.recordNodeFailure(rule, node.Name, "EvaluationError", err.Error())
		}

		// Persist the rule status
		log.V(4).Info("Attempting to persist rule status",
			"node", node.Name,
			"rule", rule.Name,
			"resourceVersion", rule.ResourceVersion)

		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latestRule := &readinessv1alpha1.NodeReadinessRule{}
			if err := r.Get(ctx, client.ObjectKey{Name: rule.Name}, latestRule); err != nil {
				return err
			}

			// update only this specific node evaluation status
			currEval := readinessv1alpha1.NodeEvaluation{}
			for _, eval := range rule.Status.NodeEvaluations {
				if eval.NodeName == node.Name {
					currEval = eval
					break
				}
			}

			found := false
			for i := range latestRule.Status.NodeEvaluations {
				if latestRule.Status.NodeEvaluations[i].NodeName == node.Name {
					latestRule.Status.NodeEvaluations[i] = currEval
					found = true
					break
				}
			}
			if !found {
				latestRule.Status.NodeEvaluations = append(
					latestRule.Status.NodeEvaluations,
					currEval,
				)
			}

			// handle status.FailedNodes for this node
			var updatedFailedNodes []readinessv1alpha1.NodeFailure
			for _, failure := range latestRule.Status.FailedNodes {
				if failure.NodeName != node.Name {
					updatedFailedNodes = append(updatedFailedNodes, failure)
				}
			}
			for _, failure := range rule.Status.FailedNodes {
				if failure.NodeName == node.Name {
					updatedFailedNodes = append(updatedFailedNodes, failure)
				}
			}
			latestRule.Status.FailedNodes = updatedFailedNodes

			return r.Status().Update(ctx, latestRule)
		})

		if err != nil {
			log.Error(err, "Failed to update rule status after node evaluation",
				"node", node.Name,
				"rule", rule.Name,
				"resourceVersion", rule.ResourceVersion)
			// continue with other rules
		} else {
			log.V(4).Info("Successfully persisted rule status from node reconciler",
				"node", node.Name,
				"rule", rule.Name,
				"newResourceVersion", rule.ResourceVersion)
		}
	}
}

// getConditionStatus gets the status of a condition on a node.
func (r *RuleReadinessController) getConditionStatus(node *corev1.Node, conditionType string) corev1.ConditionStatus {
	for _, condition := range node.Status.Conditions {
		if string(condition.Type) == conditionType {
			return condition.Status
		}
	}
	return corev1.ConditionUnknown
}

// hasTaintBySpec checks if a node has a specific taint.
func (r *RuleReadinessController) hasTaintBySpec(node *corev1.Node, taintSpec corev1.Taint) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == taintSpec.Key && taint.Effect == taintSpec.Effect {
			return true
		}
	}
	return false
}

// addTaintBySpec adds a taint to a node.
func (r *RuleReadinessController) addTaintBySpec(ctx context.Context, node *corev1.Node, taintSpec corev1.Taint) error {
	patch := client.StrategicMergeFrom(node.DeepCopy())
	node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{
		Key:    taintSpec.Key,
		Value:  taintSpec.Value,
		Effect: taintSpec.Effect,
	})
	return r.Patch(ctx, node, patch)
}

// removeTaintBySpec removes a taint from a node.
func (r *RuleReadinessController) removeTaintBySpec(ctx context.Context, node *corev1.Node, taintSpec corev1.Taint) error {
	patch := client.StrategicMergeFrom(node.DeepCopy())
	var newTaints []corev1.Taint
	for _, taint := range node.Spec.Taints {
		if taint.Key != taintSpec.Key || taint.Effect != taintSpec.Effect {
			newTaints = append(newTaints, taint)
		}
	}
	node.Spec.Taints = newTaints
	return r.Patch(ctx, node, patch)
}

// Bootstrap completion tracking.
func (r *RuleReadinessController) isBootstrapCompleted(nodeName, ruleName string) bool {
	// Check node annotation
	node := &corev1.Node{}
	if err := r.Get(context.TODO(), client.ObjectKey{Name: nodeName}, node); err != nil {
		return false
	}

	annotationKey := fmt.Sprintf("readiness.k8s.io/bootstrap-completed-%s", ruleName)
	_, exists := node.Annotations[annotationKey]
	return exists
}

func (r *RuleReadinessController) markBootstrapCompleted(ctx context.Context, nodeName, ruleName string) {
	log := ctrl.LoggerFrom(ctx)

	annotationKey := fmt.Sprintf("readiness.k8s.io/bootstrap-completed-%s", ruleName)

	// retry to handle conflict with concurrent node updates
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		node := &corev1.Node{}
		if err := r.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
			return err
		}

		// Check if already marked to avoid unnecessary updates
		if node.Annotations != nil {
			if _, exists := node.Annotations[annotationKey]; exists {
				return nil
			}
		}

		// Initialize annotations if nil
		if node.Annotations == nil {
			node.Annotations = make(map[string]string)
		}

		node.Annotations[annotationKey] = "true"
		// TODO: consider replacing this with SSA
		return r.Update(ctx, node)
	})

	if err != nil {
		log.Error(err, "Failed to mark bootstrap completed", "node", nodeName, "rule", ruleName)
	} else {
		log.Info("Marked bootstrap completed", "node", nodeName, "rule", ruleName)
	}
}

// recordNodeFailure records a failure for a specific node.
func (r *RuleReadinessController) recordNodeFailure(
	rule *readinessv1alpha1.NodeReadinessRule,
	nodeName, reason, message string,
) {
	// Remove any existing failure for this node
	var failedNodes []readinessv1alpha1.NodeFailure
	for _, failure := range rule.Status.FailedNodes {
		if failure.NodeName != nodeName {
			failedNodes = append(failedNodes, failure)
		}
	}

	// Add new failure
	failedNodes = append(failedNodes, readinessv1alpha1.NodeFailure{
		NodeName:           nodeName,
		Reason:             reason,
		Message:            message,
		LastEvaluationTime: metav1.Now(),
	})

	rule.Status.FailedNodes = failedNodes
}
