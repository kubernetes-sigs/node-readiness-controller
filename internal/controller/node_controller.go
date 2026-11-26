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
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/priorityqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
	"sigs.k8s.io/node-readiness-controller/internal/metrics"
)

// ruleFanOutPriority is the queue priority of nodes enqueued because a rule changed. It is
// below the default priority (0) of real node events, so a node that just changed is never
// stuck behind the fan-out of a rule that selects hundreds of nodes, and above the priority
// controller-runtime gives to the initial list and resyncs (handler.LowPriority).
const ruleFanOutPriority = -10

// NodeReconciler reconciles a Node object. It is the only writer of Nodes
// (taints, bootstrap annotations) and NodeReadinessEvaluations: it enforces every
// applicable rule on a node whenever the node or one of the rules selecting it changes.
type NodeReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Controller              *RuleReadinessController
	MaxConcurrentReconciles int // caps how many nodes are reconciled concurrently
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	concurrency := max(r.MaxConcurrentReconciles, 1)
	return ctrl.NewControllerManagedBy(mgr).
		Named("node").
		WithOptions(controller.Options{MaxConcurrentReconciles: concurrency}).
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
		// A rule created, changed or deleted re-evaluates every node it selects.
		Watches(&readinessv1alpha1.NodeReadinessRule{},
			r.ruleFanOutHandler(),
			builder.WithPredicates(rulePredicate())).
		Complete(r)
}

// ruleFanOutHandler enqueues the nodes selected by a changed rule at ruleFanOutPriority.
// Without a priority queue the nodes are added with the default priority.
func (r *NodeReconciler) ruleFanOutHandler() handler.EventHandler {
	enqueue := func(ctx context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request], rule client.Object) {
		requests := r.ruleToNodes(ctx, rule)
		if pq, ok := q.(priorityqueue.PriorityQueue[reconcile.Request]); ok {
			pq.AddWithOpts(priorityqueue.AddOpts{Priority: new(ruleFanOutPriority)}, requests...)
			return
		}
		for _, req := range requests {
			q.Add(req)
		}
	}

	return handler.Funcs{
		CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, q, e.Object)
		},
		// The node selector is immutable, so the new object selects the same nodes as the old one.
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, q, e.ObjectNew)
		},
		DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, q, e.Object)
		},
	}
}

// rulePredicate lets through the rule changes the node controller must act on: spec
// changes and deletion start, finalizer registration and rule removal.
//
// Finalizer removal is deliberately ignored: it is the last update before the rule is
// deleted, and the delete event that follows already re-evaluates the nodes (for example
// so the NodeReadinessEvaluation drops the rule), so reacting to both would fan out twice.
func rulePredicate() predicate.Predicate {
	return predicate.Funcs{
		// A rule without the finalizer is not enforced yet, and the update that admits it
		// fans out, so reacting to its creation would enqueue the nodes twice. A rule that
		// is created with the finalizer already set (a restore or a GitOps apply) never
		// gets that update, so its creation is the only trigger. Rules listed at startup
		// also pass; the queue de-duplicates them against the initial node list.
		CreateFunc: func(e event.CreateEvent) bool {
			return controllerutil.ContainsFinalizer(e.Object, finalizerName)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() ||
				(!controllerutil.ContainsFinalizer(e.ObjectOld, finalizerName) &&
					controllerutil.ContainsFinalizer(e.ObjectNew, finalizerName))
		},
	}
}

// ruleToNodes maps a rule to the nodes it selects.
func (r *NodeReconciler) ruleToNodes(ctx context.Context, obj client.Object) []reconcile.Request {
	log := ctrl.LoggerFrom(ctx)

	rule, ok := obj.(*readinessv1alpha1.NodeReadinessRule)
	if !ok {
		return nil
	}
	selector, err := parseNodeSelector(rule)
	if err != nil {
		log.Error(err, "Invalid node selector for rule", "rule", rule.Name)
		return nil
	}

	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList, client.MatchingLabelsSelector{Selector: selector}, client.UnsafeDisableDeepCopy); err != nil {
		log.Error(err, "Failed to list nodes for rule", "rule", rule.Name)
		return nil
	}

	requests := make([]reconcile.Request, 0, len(nodeList.Items))
	for i := range nodeList.Items {
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: nodeList.Items[i].Name}})
	}
	log.V(4).Info("Rule change enqueued nodes", "rule", rule.Name, "nodes", len(requests))
	return requests
}

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get

// NodeReconciler handles node changes

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling node", "node", req.Name)

	// Fetch the node
	node := &corev1.Node{}
	if err := r.Get(ctx, req.NamespacedName, node); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Process node against all applicable rules. A failing rule must not hide the outcome of
	// the others, so the NRE is refreshed before the error is returned for a retry.
	processErr := r.Controller.processNodeAgainstAllRules(ctx, node)

	// Update the NodeReadinessEvaluation for this node.
	if r.Controller.EnableNRE {
		r.Controller.updateNREForNode(ctx, node)
	}

	return ctrl.Result{}, processErr
}

// processNodeAgainstAllRules enforces every rule selecting the node. Rules are read from
// the informer cache; nothing here writes to a NodeReadinessRule, the
// RuleReconciler derives rule status from the resulting node state.
func (r *RuleReadinessController) processNodeAgainstAllRules(ctx context.Context, node *corev1.Node) error {
	log := ctrl.LoggerFrom(ctx)

	rules, err := r.listRulesReadOnly(ctx)
	if err != nil {
		return fmt.Errorf("failed to list rules: %w", err)
	}

	var errs []error
	for _, rule := range rules {
		if !r.ruleAppliesTo(ctx, rule, node) {
			continue
		}

		switch {
		case !rule.DeletionTimestamp.IsZero():
			// A deleting rule must hand its taint back; RuleReconciler keeps the
			// finalizer until no selected node carries it anymore.
			if err := r.releaseRuleTaint(ctx, node, rule); err != nil {
				log.Error(err, "Failed to release taint of deleting rule", "node", node.Name, "rule", rule.Name)
				errs = append(errs, err)
			}
			continue
		case !controllerutil.ContainsFinalizer(rule, finalizerName):
			// Not admitted yet: RuleReconciler registers the finalizer first so a
			// taint is never added by a rule that could not be cleaned up.
			log.V(4).Info("Skipping rule without finalizer", "node", node.Name, "rule", rule.Name)
			continue
		case rule.Spec.DryRun:
			log.V(4).Info("Skipping rule - dry run mode", "node", node.Name, "rule", rule.Name)
			continue
		case rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly &&
			nodeHasBootstrapAnnotation(node, rule):
			log.V(4).Info("Skipping bootstrap-only rule - already completed", "node", node.Name, "rule", rule.Name)
			continue
		}

		if err := r.evaluateRuleForNode(ctx, rule, node); err != nil {
			// Continue with the remaining rules; the error requeues the node with backoff.
			log.Error(err, "Failed to evaluate rule for node", "node", node.Name, "rule", rule.Name)
			metrics.Failures.WithLabelValues(rule.Name, string(metrics.FailureReasonEvaluationError)).Inc()
			r.EventRecorder.Eventf(node, nil, corev1.EventTypeWarning, "EvaluationFailed", "EvaluateRule",
				"Failed to enforce rule '%s': %v", rule.Name, err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// releaseRuleTaint removes the taint owned by a deleting rule from the node.
func (r *RuleReadinessController) releaseRuleTaint(ctx context.Context, node *corev1.Node, rule *readinessv1alpha1.NodeReadinessRule) error {
	if !r.hasTaintBySpec(node, rule.Spec.Taint) {
		return nil
	}
	ctrl.LoggerFrom(ctx).Info("Removing taint from node during rule cleanup",
		"node", node.Name, "rule", rule.Name, "taint", rule.Spec.Taint.Key)
	if err := r.removeTaintBySpec(ctx, node, rule.Spec.Taint, rule.Name); err != nil {
		return fmt.Errorf("node %s: failed to remove taint of rule %s: %w", node.Name, rule.Name, err)
	}
	return nil
}

// getConditionStatus gets the status of a condition on a node.
// If the condition is not present, defaultStatus is returned with found=false.
func (r *RuleReadinessController) getConditionStatus(
	node *corev1.Node,
	conditionType string,
	defaultStatus corev1.ConditionStatus,
) (corev1.ConditionStatus, bool) {
	for _, condition := range node.Status.Conditions {
		if string(condition.Type) == conditionType {
			return condition.Status, true
		}
	}
	return defaultStatus, false
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

// addTaintBySpec adds the rule's taint to a node. It returns whether the
// taint was added. For bootstrap-only rule, if completion annotation is
// already on the node, the add is refused.
// We use client.MergeFromWithOptimisticLock because patching a list with a
// JSON merge patch can cause races due to the fact that it fully replaces
// the list on a change. Optimistic locking ensures the patch fails with a
// conflict error if the node was modified concurrently, allowing the
// controller to retry with fresh state.
func (r *RuleReadinessController) addTaintBySpec(ctx context.Context, node *corev1.Node, rule *readinessv1alpha1.NodeReadinessRule) (bool, error) {
	log := ctrl.LoggerFrom(ctx)
	taintSpec := rule.Spec.Taint
	added := false

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		added = false

		// Fetch latest node state
		latestNode := &corev1.Node{}
		if err := r.Get(ctx, client.ObjectKey{Name: node.Name}, latestNode); err != nil {
			return err
		}

		// Check if taint already exists
		if r.hasTaintBySpec(latestNode, taintSpec) {
			return nil
		}

		if rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly &&
			nodeHasBootstrapAnnotation(latestNode, rule) {
			log.Info("Skipping taint addition - bootstrap already completed",
				"node", latestNode.Name, "rule", rule.Name, "taint", taintSpec.Key)
			return nil
		}

		stored := latestNode.DeepCopy()
		latestNode.Spec.Taints = append(latestNode.Spec.Taints, taintSpec)
		if err := r.Patch(ctx, latestNode, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}

		message := fmt.Sprintf("Taint '%s:%s' added by rule '%s'", taintSpec.Key, taintSpec.Effect, rule.Name)
		r.EventRecorder.Eventf(latestNode, nil, corev1.EventTypeNormal, "TaintAdded", "AddTaint", "%s", message)

		// Update the original node reference with the latest state
		*node = *latestNode

		added = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return added, nil
}

// removeTaintBySpec removes a taint from a node.
func (r *RuleReadinessController) removeTaintBySpec(ctx context.Context, node *corev1.Node, taintSpec corev1.Taint, ruleName string) error {
	_, err := r.removeTaint(ctx, node, taintSpec, ruleName, nil)
	return err
}

// removeTaintAndCompleteBootstrap removes the rule's taint and writes the
// bootstrap completion annotation.
func (r *RuleReadinessController) removeTaintAndCompleteBootstrap(ctx context.Context, node *corev1.Node, rule *readinessv1alpha1.NodeReadinessRule) error {
	log := ctrl.LoggerFrom(ctx)

	annotations := map[string]string{
		bootstrapAnnotationKey(rule.GetUID()): bootstrapAnnotationValue(rule.Name),
	}
	marked, err := r.removeTaint(ctx, node, rule.Spec.Taint, rule.Name, annotations)
	if err != nil {
		return err
	}
	if marked {
		log.Info("Marked bootstrap completed", "node", node.Name, "rule", rule.Name, "uid", rule.GetUID())
		metrics.BootstrapCompleted.WithLabelValues(rule.Name).Inc()
	}
	return nil
}

// removeTaint removes taintSpec from the node and sets any of the given
// annotations if not already present atomically in the same patch
// It returns whether any annotation was newly written.
// We use client.MergeFromWithOptimisticLock because patching a list with a
// JSON merge patch can cause races due to the fact that it fully replaces
// the list on a change. Optimistic locking ensures the patch fails with a
// conflict error if the node was modified concurrently, allowing the
// controller to retry with fresh state.
func (r *RuleReadinessController) removeTaint(ctx context.Context, node *corev1.Node, taintSpec corev1.Taint, ruleName string, annotations map[string]string) (bool, error) {
	hasNewAnnotations := false
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Fetch latest node state
		latestNode := &corev1.Node{}
		if err := r.Get(ctx, client.ObjectKey{Name: node.Name}, latestNode); err != nil {
			return err
		}

		hasTaint := r.hasTaintBySpec(latestNode, taintSpec)
		var missing []string
		for key := range annotations {
			if _, exists := latestNode.Annotations[key]; !exists {
				missing = append(missing, key)
			}
		}

		hasNewAnnotations = len(missing) > 0
		// Check if taint is already absent and no annotations to add
		if !hasTaint && !hasNewAnnotations {
			return nil
		}

		stored := latestNode.DeepCopy()
		if hasTaint {
			var newTaints []corev1.Taint
			for _, taint := range latestNode.Spec.Taints {
				if taint.Key != taintSpec.Key || taint.Effect != taintSpec.Effect {
					newTaints = append(newTaints, taint)
				}
			}
			latestNode.Spec.Taints = newTaints
		}
		if latestNode.Annotations == nil && hasNewAnnotations {
			latestNode.Annotations = make(map[string]string)
		}
		for _, key := range missing {
			latestNode.Annotations[key] = annotations[key]
		}
		if err := r.Patch(ctx, latestNode, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}

		if hasTaint {
			message := fmt.Sprintf("Taint '%s:%s' removed by rule '%s'", taintSpec.Key, taintSpec.Effect, ruleName)
			r.EventRecorder.Eventf(latestNode, nil, corev1.EventTypeNormal, "TaintRemoved", "RemoveTaint", "%s", message)
		}

		// Update the original node reference with the latest state
		*node = *latestNode

		return nil
	})
	if err != nil {
		return false, err
	}
	return hasNewAnnotations, nil
}

// nodeHasBootstrapAnnotation reports whether the given node object carries
// the rule's bootstrap completion annotation (UID-based or legacy key).
func nodeHasBootstrapAnnotation(node *corev1.Node, rule *readinessv1alpha1.NodeReadinessRule) bool {
	_, existsNew := node.Annotations[bootstrapAnnotationKey(rule.GetUID())]
	_, existsLegacy := node.Annotations[legacyBootstrapAnnotationKey(rule.Name)]
	return existsNew || existsLegacy
}

// markBootstrapCompleted records bootstrap completion for a rule when a
// node didnt have a taint remove action. For the nodes already carrying taint,
// it is deferred and handled eventually by removeTaintAndCompleteBootstrap.
// On success node is refreshed to the latest observed state (like addTaintBySpec and
// removeTaint do), and a failure is returned so the node is requeued with backoff.
func (r *RuleReadinessController) markBootstrapCompleted(ctx context.Context, node *corev1.Node, rule *readinessv1alpha1.NodeReadinessRule) error {
	log := ctrl.LoggerFrom(ctx)
	marked := false
	deferred := false
	annotationKey := bootstrapAnnotationKey(rule.GetUID())
	latestNode := &corev1.Node{}

	// The optimistic lock here protects from a race-condition adding a taint between hasTaintBySpec
	// check and mark completed annotation patch from concurrent reconciliations.
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		marked = false
		deferred = false

		latestNode = &corev1.Node{}
		if err := r.Get(ctx, client.ObjectKey{Name: node.Name}, latestNode); err != nil {
			return err
		}

		// Check if already marked to avoid unnecessary updates.
		if _, exists := latestNode.Annotations[annotationKey]; exists {
			return nil
		}

		if r.hasTaintBySpec(latestNode, rule.Spec.Taint) {
			deferred = true
			return nil
		}

		stored := latestNode.DeepCopy()

		// Initialize annotations map if nil.
		if latestNode.Annotations == nil {
			latestNode.Annotations = make(map[string]string)
		}

		latestNode.Annotations[annotationKey] = bootstrapAnnotationValue(rule.Name)
		if err := r.Patch(ctx, latestNode, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}

		marked = true
		return nil
	})
	if err != nil {
		log.Error(err, "Failed to mark bootstrap completed", "node", node.Name, "rule", rule.Name, "uid", rule.GetUID())
		metrics.Failures.WithLabelValues(rule.Name, string(metrics.FailureReasonAnnotationPatchFailed)).Inc()
		return fmt.Errorf("failed to mark bootstrap completed: %w", err)
	}

	// Update the original node reference with the latest state.
	*node = *latestNode

	switch {
	case deferred:
		log.Info("Deferring bootstrap completion - rule taint still present on node",
			"node", node.Name, "rule", rule.Name, "taint", rule.Spec.Taint.Key)
	case marked:
		log.Info("Marked bootstrap completed", "node", node.Name, "rule", rule.Name, "uid", rule.GetUID())
		metrics.BootstrapCompleted.WithLabelValues(rule.Name).Inc()
	default:
		log.V(4).Info("Bootstrap already completed", "node", node.Name, "rule", rule.Name, "uid", rule.GetUID())
	}
	return nil
}

// evaluateRuleForNode evaluates a single rule against a single node.
func (r *RuleReadinessController) evaluateRuleForNode(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, node *corev1.Node) error {
	timer := prometheus.NewTimer(metrics.EvaluationDuration.WithLabelValues(rule.Name))
	defer timer.ObserveDuration()
	log := ctrl.LoggerFrom(ctx)

	// Determine taint action: allOf requires every condition satisfied; anyOf requires at least one.
	outcomes, shouldRemoveTaint := r.evaluateRuleConditions(node, rule)
	for i, outcome := range outcomes {
		condReq := rule.Spec.Conditions[i]
		if !outcome.satisfied {
			metrics.ConditionEvaluationFailures.WithLabelValues(rule.Name, condReq.Type).Inc()
		}
		log.V(1).Info("Condition evaluation", "node", node.Name, "rule", rule.Name,
			"conditionType", condReq.Type, "observed", outcome.result.CurrentStatus,
			"effective", outcome.effective, "required", condReq.RequiredStatus,
			"satisfied", outcome.satisfied)
	}
	currentlyHasTaint := r.hasTaintBySpec(node, rule.Spec.Taint)

	log.Info("Evaluation result", "node", node.Name, "rule", rule.Name,
		"conditionPolicy", rule.Spec.GetConditionPolicy(), "conditionsSatisfied", shouldRemoveTaint, "hasTaint", currentlyHasTaint)

	isFirstEvaluation := r.getPreviousNodeEvaluation(rule, node.Name) == nil

	// Calculate the latest transition time globally so all metrics can share it.
	// We intentionally isolate the most recent transition time among all required conditions.
	// Since the controller must wait for the combined state of all conditions to change
	// before taking action, the condition that changed most recently is the "trigger" event.
	var latestTransition metav1.Time
	for _, req := range rule.Spec.Conditions {
		for _, cond := range node.Status.Conditions {
			if string(cond.Type) == req.Type && cond.LastTransitionTime.After(latestTransition.Time) {
				latestTransition = cond.LastTransitionTime
			}
		}
	}

	recordLatency := func(operation metrics.ReconciliationOperation, enforcementOperation metrics.EnforcementOperation) {
		if !latestTransition.IsZero() {
			latency := time.Since(latestTransition.Time).Seconds()

			// Protect against NTP clock drift between the node and controller.
			// If the node's clock is ahead, latency will be negative.
			if latency < 0 {
				latency = 0
			}

			// Deprecated: ReconciliationLatency is superseded by EnforcementLatency and will be removed in future releases.
			//nolint:staticcheck
			metrics.ReconciliationLatency.WithLabelValues(rule.Name, string(operation)).Observe(latency)
			metrics.EnforcementLatency.WithLabelValues(rule.Name, string(enforcementOperation)).Observe(latency)
		}
	}

	var err error

	switch {
	case shouldRemoveTaint && currentlyHasTaint:
		log.Info("Removing taint", "node", node.Name, "rule", rule.Name, "taint", rule.Spec.Taint.Key)

		if rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly {
			err = r.removeTaintAndCompleteBootstrap(ctx, node, rule)
		} else {
			err = r.removeTaintBySpec(ctx, node, rule.Spec.Taint, rule.Name)
		}
		if err != nil {
			metrics.Failures.WithLabelValues(rule.Name, string(metrics.FailureReasonRemoveTaintError)).Inc()
			return fmt.Errorf("failed to remove taint: %w", err)
		}

		// Record taint removal latency and taint operation counter.
		metrics.TaintOperations.WithLabelValues(rule.Name, string(metrics.TaintOperationRemove)).Inc()
		recordLatency(metrics.ReconciliationOperationRemoveTaint, metrics.EnforcementOperationRemove)

		if rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly {
			// Only record the bootstrap duration if the node was created after the rule.
			// This prevents legacy nodes from poisoning the histogram with massive outliers.
			if !node.CreationTimestamp.Time.Before(rule.CreationTimestamp.Time) && !latestTransition.IsZero() {
				// Use only API-server-generated timestamps to avoid Controller/Node clock skew
				duration := latestTransition.Time.Sub(node.CreationTimestamp.Time).Seconds()

				if duration > 0 {
					metrics.BootstrapDuration.WithLabelValues(rule.Name).Observe(duration)
				}
			} else {
				log.V(4).Info("Skipping bootstrap duration metric for legacy node or missing transition",
					"node", node.Name,
					"rule", rule.Name)
			}
		}

	case !shouldRemoveTaint && !currentlyHasTaint:
		log.Info("Adding taint", "node", node.Name, "rule", rule.Name, "taint", rule.Spec.Taint.Key)

		var added bool
		if added, err = r.addTaintBySpec(ctx, node, rule); err != nil {
			metrics.Failures.WithLabelValues(rule.Name, string(metrics.FailureReasonAddTaintError)).Inc()
			return fmt.Errorf("failed to add taint: %w", err)
		}

		if added {
			// Record add taint latency and taint operation counter
			metrics.TaintOperations.WithLabelValues(rule.Name, string(metrics.TaintOperationAdd)).Inc()
			recordLatency(metrics.ReconciliationOperationAddTaint, metrics.EnforcementOperationAdd)
		}

	case !shouldRemoveTaint && currentlyHasTaint:
		if isFirstEvaluation {
			log.Info("Adopting pre-existing taint", "node", node.Name, "rule", rule.Name, "taint", rule.Spec.Taint.Key)

			message := fmt.Sprintf("Taint '%s:%s' is now managed by rule '%s'", rule.Spec.Taint.Key, rule.Spec.Taint.Effect, rule.Name)
			r.EventRecorder.Eventf(node, nil, corev1.EventTypeNormal, "TaintAdopted", "AdoptTaint", "%s", message)
		}

	default:
		log.Info("No taint action needed", "node", node.Name, "rule", rule.Name,
			"shouldRemove", shouldRemoveTaint, "hasTaint", currentlyHasTaint)
		// Mark bootstrap completed in bootstrap-only mode when conditions satisfied even if taint is already absent.
		if rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly {
			return r.markBootstrapCompleted(ctx, node, rule)
		}
	}

	return nil
}
