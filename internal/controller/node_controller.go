/*
Copyright 2025.

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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	readinessv1alpha1 "github.com/ajaysundark/node-readiness-gate-controller/api/v1alpha1"
)

// NodeReconciler reconciles a Node object
type NodeReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Controller *ReadinessGateController
}

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=nodes/finalizers,verbs=update

// NodeReconciler handles node changes

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the node
	node := &corev1.Node{}
	if err := r.Get(ctx, req.NamespacedName, node); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Process node against all applicable rules
	if err := r.Controller.processNodeAgainstAllRules(ctx, node); err != nil {
		log.Error(err, "Failed to process node", "node", node.Name)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	return ctrl.Result{}, nil
}

// processNodeAgainstAllRules processes a single node against all applicable rules
func (r *ReadinessGateController) processNodeAgainstAllRules(ctx context.Context, node *corev1.Node) error {
	log := ctrl.LoggerFrom(ctx)

	// Get all applicable rules for this node
	applicableRules := r.getApplicableRulesForNode(node)

	for _, rule := range applicableRules {
		// Skip if bootstrap-only and already completed
		if r.isBootstrapCompleted(node.Name, rule.Name) && rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly {
			log.V(4).Info("Skipping bootstrap-only rule - already completed",
				"node", node.Name, "rule", rule.Name)
			continue
		}

		// Skip if dry run or global dry run
		if rule.Spec.DryRun || r.globalDryRun {
			log.V(4).Info("Skipping rule - dry run mode",
				"node", node.Name, "rule", rule.Name)
			continue
		}

		if err := r.evaluateRuleForNode(ctx, rule, node); err != nil {
			log.Error(err, "Failed to evaluate rule for node",
				"node", node.Name, "rule", rule.Name)
			// Continue with other rules even if one fails
			r.recordNodeFailure(rule, node.Name, "EvaluationError", err.Error())
		}
	}

	return nil
}

// getConditionStatus gets the status of a condition on a node
func (r *ReadinessGateController) getConditionStatus(node *corev1.Node, conditionType string) corev1.ConditionStatus {
	for _, condition := range node.Status.Conditions {
		if string(condition.Type) == conditionType {
			return condition.Status
		}
	}
	return corev1.ConditionUnknown
}

// hasTaintBySpec checks if a node has a specific taint
func (r *ReadinessGateController) hasTaintBySpec(node *corev1.Node, taintSpec readinessv1alpha1.TaintSpec) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == taintSpec.Key && taint.Effect == taintSpec.Effect {
			return true
		}
	}
	return false
}

// addTaintBySpec adds a taint to a node
func (r *ReadinessGateController) addTaintBySpec(ctx context.Context, node *corev1.Node, taintSpec readinessv1alpha1.TaintSpec) error {
	patch := client.StrategicMergeFrom(node.DeepCopy())
	node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{
		Key:    taintSpec.Key,
		Value:  taintSpec.Value,
		Effect: taintSpec.Effect,
	})
	return r.Patch(ctx, node, patch)
}

// removeTaintBySpec removes a taint from a node
func (r *ReadinessGateController) removeTaintBySpec(ctx context.Context, node *corev1.Node, taintSpec readinessv1alpha1.TaintSpec) error {
	patch := client.StrategicMergeFrom(node.DeepCopy())
	var newTaints []corev1.Taint
	for _, taint := range node.Spec.Taints {
		if !(taint.Key == taintSpec.Key && taint.Effect == taintSpec.Effect) {
			newTaints = append(newTaints, taint)
		}
	}
	node.Spec.Taints = newTaints
	return r.Patch(ctx, node, patch)
}

// Bootstrap completion tracking
func (r *ReadinessGateController) isBootstrapCompleted(nodeName, ruleName string) bool {
	// Check node annotation
	node := &corev1.Node{}
	if err := r.Get(context.TODO(), client.ObjectKey{Name: nodeName}, node); err != nil {
		return false
	}

	annotationKey := fmt.Sprintf("readiness.k8s.io/bootstrap-completed-%s", ruleName)
	_, exists := node.Annotations[annotationKey]
	return exists
}

func (r *ReadinessGateController) markBootstrapCompleted(ctx context.Context, nodeName, ruleName string) {
	node := &corev1.Node{}
	if err := r.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		klog.Errorf("Failed to get node %s for bootstrap completion: %v", nodeName, err)
		return
	}

	annotationKey := fmt.Sprintf("readiness.k8s.io/bootstrap-completed-%s", ruleName)

	// Initialize annotations if nil
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}

	node.Annotations[annotationKey] = "true"

	if err := r.Update(ctx, node); err != nil {
		klog.Errorf("Failed to mark bootstrap completed for node %s rule %s: %v", nodeName, ruleName, err)
	}
}

// updateNodeEvaluationStatus updates the evaluation status for a specific node
func (r *ReadinessGateController) updateNodeEvaluationStatus(
	rule *readinessv1alpha1.NodeReadinessGateRule,
	nodeName string,
	conditionResults []readinessv1alpha1.ConditionEvaluationResult,
	action string,
) {
	// Find existing evaluation or create new
	var nodeEval *readinessv1alpha1.NodeEvaluation
	for i := range rule.Status.NodeEvaluations {
		if rule.Status.NodeEvaluations[i].NodeName == nodeName {
			nodeEval = &rule.Status.NodeEvaluations[i]
			break
		}
	}

	if nodeEval == nil {
		rule.Status.NodeEvaluations = append(rule.Status.NodeEvaluations, readinessv1alpha1.NodeEvaluation{
			NodeName: nodeName,
		})
		nodeEval = &rule.Status.NodeEvaluations[len(rule.Status.NodeEvaluations)-1]
	}

	// Update evaluation
	nodeEval.ConditionResults = conditionResults
	nodeEval.TaintAction = action
	nodeEval.LastEvaluated = metav1.Now()
}

// recordNodeFailure records a failure for a specific node
func (r *ReadinessGateController) recordNodeFailure(
	rule *readinessv1alpha1.NodeReadinessGateRule,
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
		NodeName:    nodeName,
		Reason:      reason,
		Message:     message,
		LastUpdated: metav1.Now(),
	})

	rule.Status.FailedNodes = failedNodes
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	nodeController, err := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Named("node").
		Build(r)
	if err != nil {
		return err
	}
	// Watch node create/update events for readiness taint processing
	return nodeController.Watch(
		source.Kind(mgr.GetCache(), &corev1.Node{},
			&handler.TypedEnqueueRequestForObject[*corev1.Node]{},
			predicate.TypedFuncs[*corev1.Node]{
				CreateFunc: func(e event.TypedCreateEvent[*corev1.Node]) bool {
					log := ctrl.LoggerFrom(ctx)
					log.Info("NodeReconciler processing node create event", "node", e.Object.Name)
					return true
				},
				UpdateFunc: func(e event.TypedUpdateEvent[*corev1.Node]) bool {
					log := ctrl.LoggerFrom(ctx)
					log.Info("NodeReconciler processing node update event", "node", e.ObjectNew.Name)
					// Reconcile if the resource version has changed.
					return e.ObjectOld.GetResourceVersion() != e.ObjectNew.GetResourceVersion()
				},
			}))
}
