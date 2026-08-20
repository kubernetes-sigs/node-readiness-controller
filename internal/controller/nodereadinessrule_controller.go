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
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
	"sigs.k8s.io/node-readiness-controller/internal/metrics"
)

const (
	// finalizerName is the finalizer added to NodeReadinessRule to ensure cleanup.
	finalizerName = "readiness.node.x-k8s.io/cleanup-taints"
)

// RuleReadinessController manages node taints based on readiness rules.
type RuleReadinessController struct {
	client.Client
	Scheme                 *runtime.Scheme
	clientset              kubernetes.Interface
	EventRecorder          events.EventRecorder
	EnableNodeStateMetrics bool

	// Cache for efficient rule lookup
	ruleCacheMutex sync.RWMutex
	ruleCache      map[string]*readinessv1alpha1.NodeReadinessRule // ruleName -> rule
}

// RuleReconciler handles NodeReadinessRule reconciliation.
type RuleReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Controller              *RuleReadinessController
	MaxConcurrentReconciles int // caps how many rules are reconciled concurrently
}

// NewRuleReadinessController creates a new controller.
func NewRuleReadinessController(mgr ctrl.Manager, clientset kubernetes.Interface, enableNodeStateMetrics bool) *RuleReadinessController {
	return &RuleReadinessController{
		Client:                 mgr.GetClient(),
		Scheme:                 mgr.GetScheme(),
		clientset:              clientset,
		EventRecorder:          mgr.GetEventRecorder("node-readiness-controller"),
		EnableNodeStateMetrics: enableNodeStateMetrics,
		ruleCache:              make(map[string]*readinessv1alpha1.NodeReadinessRule),
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *RuleReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	concurrency := max(r.MaxConcurrentReconciles, 1)
	return ctrl.NewControllerManagedBy(mgr).
		Named("nodereadiness-controller").
		WithOptions(controller.Options{MaxConcurrentReconciles: concurrency}).
		For(&readinessv1alpha1.NodeReadinessRule{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrules,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrules/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

func (r *RuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling rule", "rule", req.Name)

	// Fetch the rule
	rule := &readinessv1alpha1.NodeReadinessRule{}
	if err := r.Get(ctx, req.NamespacedName, rule); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Rule not found, removing from cache", "rule", req.Name)
			r.Controller.removeRuleFromCache(ctx, req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log = log.WithValues("ruleName", rule.Name)
	ctx = ctrl.LoggerInto(ctx, log)

	// Add finalizer first if not set to avoid the race condition between init and delete.
	if finalizerAdded, err := r.ensureFinalizer(ctx, rule, finalizerName); err != nil {
		return ctrl.Result{}, err
	} else if finalizerAdded {
		// Adding a finalizer modifies Metadata, not Spec, so the Generation is unchanged.
		// GenerationChangedPredicate prevents triggering a new reconcile, we must explicitly requeue to proceed.
		log.V(3).Info("Finalizer added, requeuing")
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return ctrl.Result{}, err
	}

	// Handle deletion reconciliation loop.
	if !rule.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, rule, nodeList)
	}

	// Update rule cache (after cleanup)
	r.Controller.updateRuleCache(ctx, rule)

	// Handle dry run
	if rule.Spec.DryRun {
		if err := r.Controller.processDryRun(ctx, rule, nodeList); err != nil {
			log.Error(err, "Failed to process dry run", "rule", rule.Name)
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
	} else {
		// Clear previous dry run results
		rule.Status.DryRunResults = readinessv1alpha1.DryRunResults{}

		// Process all applicable nodes for this rule
		if err := r.Controller.processAllNodesForRule(ctx, rule, nodeList); err != nil {
			log.Error(err, "Failed to process nodes for rule", "rule", rule.Name)
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
	}

	// Update rule status
	if err := r.Controller.updateRuleStatus(ctx, rule); err != nil {
		log.Error(err, "Failed to update rule status", "rule", rule.Name)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Clean up status for deleted nodes
	if err := r.Controller.cleanupDeletedNodes(ctx, rule, nodeList); err != nil {
		log.Error(err, "Failed to clean up deleted nodes", "rule", rule.Name)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Update top-level rule metrics.
	metrics.RuleLastReconciliationTime.WithLabelValues(rule.Name).Set(float64(time.Now().Unix()))

	if r.Controller.EnableNodeStateMetrics {
		r.Controller.SyncNodeStateMetrics(ctx, rule)
	}

	return ctrl.Result{}, nil
}

// reconcileDelete handles the rules deletion, It performs following actions
// 1. Deletes the taints associated with the rule.
// 2. Remove the rule from the cache.
// 3. Remove the finalizer from the rule.
func (r *RuleReconciler) reconcileDelete(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, nodeList *corev1.NodeList) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Update cache with deletion-marked rule before cleanup.
	log.V(3).Info("Updating cache with deletion-marked rule before cleanup")
	r.Controller.updateRuleCache(ctx, rule)

	log.Info("Cleaning up taints for deleted rule", "rule", rule.Name)
	if err := r.Controller.cleanupTaintsForRule(ctx, rule, nodeList); err != nil {
		log.Error(err, "Failed to cleanup taints for rule", "rule", rule.Name)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	log.V(3).Info("Removing the rule from cache")
	r.Controller.removeRuleFromCache(ctx, rule.Name)

	log.V(3).Info("Removing the finalizer from the rule")
	patch := client.MergeFrom(rule.DeepCopy())
	controllerutil.RemoveFinalizer(rule, finalizerName)
	err := r.Patch(ctx, rule, patch)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Clean up metrics for deleted rule to prevent Go client memory leaks.
	ruleLabel := prometheus.Labels{"rule": rule.Name}

	// For single-label metrics, DeleteLabelValues is fine
	metrics.RuleLastReconciliationTime.DeleteLabelValues(rule.Name)
	metrics.BootstrapCompleted.DeleteLabelValues(rule.Name)
	metrics.BootstrapDuration.DeleteLabelValues(rule.Name)
	metrics.EvaluationDuration.DeleteLabelValues(rule.Name)

	// For multi-label metrics, use DeletePartialMatch to wipe all combinations
	metrics.NodesByState.DeletePartialMatch(ruleLabel)
	metrics.Failures.DeletePartialMatch(ruleLabel)
	metrics.ConditionEvaluationFailures.DeletePartialMatch(ruleLabel)
	metrics.TaintOperations.DeletePartialMatch(ruleLabel)
	metrics.ReconciliationLatency.DeletePartialMatch(ruleLabel)

	return ctrl.Result{}, nil
}

// cleanupDeletedNodes removes status entries for nodes that no longer exist.
func (r *RuleReadinessController) cleanupDeletedNodes(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, nodeList *corev1.NodeList) error {
	log := ctrl.LoggerFrom(ctx)

	existingNodes := make(map[string]bool, len(nodeList.Items))
	for _, node := range nodeList.Items {
		existingNodes[node.Name] = true
	}

	// Filter out deleted nodes
	newNodeEvaluations, newFailedNodes := filterStatusForExistingNodes(
		existingNodes,
		rule.Status.NodeEvaluations,
		rule.Status.FailedNodes,
	)

	if len(newNodeEvaluations) == len(rule.Status.NodeEvaluations) &&
		len(newFailedNodes) == len(rule.Status.FailedNodes) {
		log.V(4).Info("No deleted nodes to clean up", "rule", rule.Name)
		return nil
	}

	log.V(4).Info("Cleaning up deleted nodes from rule status",
		"rule", rule.Name,
		"before", len(rule.Status.NodeEvaluations),
		"after", len(newNodeEvaluations))

	// Use retry on conflict to update status to avoid race conditions from node updates
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh := &readinessv1alpha1.NodeReadinessRule{}
		if err := r.Get(ctx, client.ObjectKey{Name: rule.Name}, fresh); err != nil {
			return err
		}

		freshNodeEvaluations, freshFailedNodes := filterStatusForExistingNodes(
			existingNodes,
			fresh.Status.NodeEvaluations,
			fresh.Status.FailedNodes,
		)

		if len(freshNodeEvaluations) == len(fresh.Status.NodeEvaluations) &&
			len(freshFailedNodes) == len(fresh.Status.FailedNodes) {
			return nil
		}

		patch := client.MergeFrom(fresh.DeepCopy())
		fresh.Status.NodeEvaluations = freshNodeEvaluations
		fresh.Status.FailedNodes = freshFailedNodes
		return r.Status().Patch(ctx, fresh, patch)
	})
}

// processAllNodesForRule processes all nodes when a rule changes.
//
//nolint:unparam // Keep error return for future extensibility and API stability.
func (r *RuleReadinessController) processAllNodesForRule(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, nodeList *corev1.NodeList) error {
	log := ctrl.LoggerFrom(ctx)

	log.Info("Processing all nodes for rule", "rule", rule.Name, "totalNodes", len(nodeList.Items))

	var appliedNodes []string
	for _, node := range nodeList.Items {
		if r.ruleAppliesTo(ctx, rule, &node) {
			log.Info("Processing node for rule", "rule", rule.Name, "node", node.Name)
			if err := r.evaluateRuleForNode(ctx, rule, &node); err != nil {
				log.Error(err, "Failed to evaluate node for rule", "rule", rule.Name, "node", node.Name)
				r.recordNodeFailure(rule, node.Name, "EvaluationError", err.Error())
				metrics.Failures.WithLabelValues(rule.Name, string(metrics.FailureReasonEvaluationError)).Inc()
			} else {
				appliedNodes = append(appliedNodes, node.Name)
				var updatedFailedNodes []readinessv1alpha1.NodeFailure
				for _, f := range rule.Status.FailedNodes {
					if f.NodeName != node.Name {
						updatedFailedNodes = append(updatedFailedNodes, f)
					}
				}
				rule.Status.FailedNodes = updatedFailedNodes
			}
		}
	}

	// Update status
	rule.Status.ObservedGeneration = rule.Generation
	rule.Status.AppliedNodes = appliedNodes

	if !rule.Spec.DryRun {
		rule.Status.DryRunResults = readinessv1alpha1.DryRunResults{}
	}

	log.Info("Completed processing nodes for rule", "rule", rule.Name, "processedCount", len(appliedNodes))
	return nil
}

// evaluateRuleForNode evaluates a single rule against a single node.
func (r *RuleReadinessController) evaluateRuleForNode(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, node *corev1.Node) error {
	timer := prometheus.NewTimer(metrics.EvaluationDuration.WithLabelValues(rule.Name))
	defer timer.ObserveDuration()
	log := ctrl.LoggerFrom(ctx)

	// Evaluate all conditions, accumulating the policy result alongside per-condition results.
	conditionResults := make([]readinessv1alpha1.ConditionEvaluationResult, 0, len(rule.Spec.Conditions))
	conditionPolicy := rule.Spec.GetConditionPolicy()
	allSatisfied := true
	anySatisfied := false

	for _, condReq := range rule.Spec.Conditions {
		effectiveStatus, conditionFound := r.getConditionStatus(
			node,
			condReq.Type,
			condReq.GetDefaultStatus(),
		)
		satisfied := effectiveStatus == condReq.RequiredStatus

		if !satisfied {
			allSatisfied = false
			metrics.ConditionEvaluationFailures.WithLabelValues(rule.Name, condReq.Type).Inc()
		} else {
			anySatisfied = true
		}

		// observedStatus is the condition status of a node without applying the default
		// fallback in case the condition is not found.
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

		log.V(1).Info("Condition evaluation", "node", node.Name, "rule", rule.Name,
			"conditionType", condReq.Type, "observed", observedStatus,
			"effective", effectiveStatus, "required", condReq.RequiredStatus,
			"satisfied", satisfied)
	}

	// Determine taint action: allOf requires every condition satisfied; anyOf requires at least one.
	shouldRemoveTaint := allSatisfied
	if conditionPolicy == readinessv1alpha1.ConditionPolicyAnyOf {
		shouldRemoveTaint = anySatisfied
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

	recordLatency := func(operation string) {
		if !latestTransition.IsZero() {
			latency := time.Since(latestTransition.Time).Seconds()

			// Protect against NTP clock drift between the node and controller.
			// If the node's clock is ahead, latency will be negative.
			if latency < 0 {
				latency = 0
			}

			metrics.ReconciliationLatency.WithLabelValues(rule.Name, operation).Observe(latency)
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
		recordLatency(string(metrics.ReconciliationOperationRemoveTaint))

		if rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly {
			// Only record the bootstrap duration if the node was created AFTER the rule.
			// This prevents legacy nodes from poisoning the histogram with massive outliers.
			if !node.CreationTimestamp.Time.Before(rule.CreationTimestamp.Time) && !latestTransition.IsZero() {
				// Use ONLY API-server-generated timestamps to avoid Controller/Node clock skew
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
			recordLatency(string(metrics.ReconciliationOperationAddTaint))
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
			r.markBootstrapCompleted(ctx, node.Name, rule)
		}
	}

	// Determine observed taint status after any actions
	var taintStatus readinessv1alpha1.TaintStatus
	if r.hasTaintBySpec(node, rule.Spec.Taint) {
		taintStatus = readinessv1alpha1.TaintStatusPresent
	} else {
		taintStatus = readinessv1alpha1.TaintStatusAbsent
	}

	// Update evaluation status
	r.updateNodeEvaluationStatus(rule, node.Name, conditionResults, taintStatus)

	return nil
}

// updateNodeEvaluationStatus updates the evaluation status for a specific node.
func (r *RuleReadinessController) updateNodeEvaluationStatus(
	rule *readinessv1alpha1.NodeReadinessRule,
	nodeName string,
	conditionResults []readinessv1alpha1.ConditionEvaluationResult,
	taintStatus readinessv1alpha1.TaintStatus,
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
	nodeEval.TaintStatus = taintStatus
	nodeEval.LastEvaluationTime = metav1.Now()
}

// getApplicableRulesForNode returns all rules applicable to a node.
func (r *RuleReadinessController) getApplicableRulesForNode(ctx context.Context, node *corev1.Node) []*readinessv1alpha1.NodeReadinessRule {
	r.ruleCacheMutex.RLock()
	defer r.ruleCacheMutex.RUnlock()

	var applicableRules []*readinessv1alpha1.NodeReadinessRule

	for _, rule := range r.ruleCache {
		if r.ruleAppliesTo(ctx, rule, node) {
			applicableRules = append(applicableRules, rule.DeepCopy())
		}
	}

	return applicableRules
}

// ListNodes returns the current list of Nodes.
func (r *RuleReadinessController) ListNodes(ctx context.Context) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return nil, err
	}
	return nodeList.Items, nil
}

// forEachRuleNode applies callbacks to nodes matching each rule.
func (r *RuleReadinessController) forEachRuleNode(
	ctx context.Context,
	nodes []corev1.Node,
	skipRule func(rule *readinessv1alpha1.NodeReadinessRule) bool,
	onRule func(rule *readinessv1alpha1.NodeReadinessRule),
	onNode func(rule *readinessv1alpha1.NodeReadinessRule, node *corev1.Node, held bool),
) error {
	ruleList := &readinessv1alpha1.NodeReadinessRuleList{}
	if err := r.List(ctx, ruleList); err != nil {
		return err
	}

	log := ctrl.LoggerFrom(ctx)

	for i := range ruleList.Items {
		rule := &ruleList.Items[i]
		if rule.Spec.DryRun {
			continue
		}
		if skipRule(rule) {
			continue
		}

		// Parse the selector once per rule.
		selector, err := parseNodeSelector(rule)
		if err != nil {
			log.V(2).Info("Invalid node selector for rule", "rule", rule.Name, "error", err)
			continue
		}

		onRule(rule)

		for i := range nodes {
			node := &nodes[i]
			if !selector.Matches(labels.Set(node.Labels)) {
				continue
			}
			onNode(rule, node, r.hasTaintBySpec(node, rule.Spec.Taint))
		}
	}

	return nil
}

// ListRuleNodeStates returns the number of held and released nodes for each rule.
func (r *RuleReadinessController) ListRuleNodeStates(ctx context.Context, nodes []corev1.Node) (map[string]metrics.RuleNodeCounts, error) {
	counts := make(map[string]metrics.RuleNodeCounts)

	err := r.forEachRuleNode(ctx, nodes,
		func(rule *readinessv1alpha1.NodeReadinessRule) bool { return false },
		func(rule *readinessv1alpha1.NodeReadinessRule) {
			counts[rule.Name] = metrics.RuleNodeCounts{}
		},
		func(rule *readinessv1alpha1.NodeReadinessRule, node *corev1.Node, held bool) {
			rc := counts[rule.Name]
			if held {
				rc.Held++
			} else {
				rc.Released++
			}
			counts[rule.Name] = rc
		},
	)
	if err != nil {
		return nil, err
	}

	return counts, nil
}

// ListBlockedNodes returns the number of blocked nodes for each rule and unsatisfied condition.
func (r *RuleReadinessController) ListBlockedNodes(ctx context.Context, nodes []corev1.Node) (map[string]metrics.RuleBlockedConditions, error) {
	result := make(map[string]metrics.RuleBlockedConditions)

	err := r.forEachRuleNode(ctx, nodes,
		func(rule *readinessv1alpha1.NodeReadinessRule) bool { return !rule.DeletionTimestamp.IsZero() },
		func(rule *readinessv1alpha1.NodeReadinessRule) {
			counts := make(metrics.RuleBlockedConditions, len(rule.Spec.Conditions))
			for _, cond := range rule.Spec.Conditions {
				counts[cond.Type] = 0
			}
			result[rule.Name] = counts
		},
		func(rule *readinessv1alpha1.NodeReadinessRule, node *corev1.Node, held bool) {
			if !held {
				return
			}
			counts := result[rule.Name]
			for _, cond := range rule.Spec.Conditions {
				effectiveStatus, _ := r.getConditionStatus(node, cond.Type, cond.GetDefaultStatus())
				if effectiveStatus != cond.RequiredStatus {
					counts[cond.Type]++
				}
			}
		},
	)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// parseNodeSelector parses a rule's NodeSelector into a labels.Selector.
func parseNodeSelector(rule *readinessv1alpha1.NodeReadinessRule) (labels.Selector, error) {
	return metav1.LabelSelectorAsSelector(&rule.Spec.NodeSelector)
}

// ruleAppliesTo checks if a rule applies to a node.
func (r *RuleReadinessController) ruleAppliesTo(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, node *corev1.Node) bool {
	log := ctrl.LoggerFrom(ctx)

	selector, err := parseNodeSelector(rule)
	if err != nil {
		log.Error(err, "Invalid node selector for rule", "rule", rule.Name)
		return false
	}

	return selector.Matches(labels.Set(node.Labels))
}

// checks if a rule applies to a node and has its taint.
func (r *RuleReadinessController) ruleAppliesToWithTaint(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, node *corev1.Node) (applies, held bool) {
	applies = r.ruleAppliesTo(ctx, rule, node)
	if !applies {
		return false, false
	}
	return true, r.hasTaintBySpec(node, rule.Spec.Taint)
}

// updateRuleCache updates the rule cache.
func (r *RuleReadinessController) updateRuleCache(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) {
	log := ctrl.LoggerFrom(ctx)
	r.ruleCacheMutex.Lock()
	defer r.ruleCacheMutex.Unlock()

	ruleCopy := rule.DeepCopy()
	r.ruleCache[rule.Name] = ruleCopy
	metrics.RulesTotal.Set(float64(len(r.ruleCache)))
	log.V(4).Info("Updated rule cache",
		"rule", rule.Name,
		"totalRules", len(r.ruleCache),
		"resourceVersion", ruleCopy.ResourceVersion)
}

// removeRuleFromCache removes a rule from cache.
func (r *RuleReadinessController) removeRuleFromCache(ctx context.Context, ruleName string) {
	log := ctrl.LoggerFrom(ctx)
	r.ruleCacheMutex.Lock()
	defer r.ruleCacheMutex.Unlock()

	delete(r.ruleCache, ruleName)
	metrics.RulesTotal.Set(float64(len(r.ruleCache)))
	log.Info("Removed rule from cache", "rule", ruleName, "totalRules", len(r.ruleCache))
}

// updateRuleStatus updates the status of a NodeReadinessRule.
func (r *RuleReadinessController) updateRuleStatus(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) error {
	log := ctrl.LoggerFrom(ctx)

	log.V(1).Info("Updating rule status",
		"rule", rule.Name,
		"nodeEvaluations", len(rule.Status.NodeEvaluations),
		"appliedNodes", len(rule.Status.AppliedNodes))

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latestRule := &readinessv1alpha1.NodeReadinessRule{}
		if err := r.Get(ctx, client.ObjectKey{Name: rule.Name}, latestRule); err != nil {
			return err
		}

		patch := client.MergeFrom(latestRule.DeepCopy())

		latestRule.Status.NodeEvaluations = rule.Status.NodeEvaluations
		latestRule.Status.AppliedNodes = rule.Status.AppliedNodes
		latestRule.Status.FailedNodes = rule.Status.FailedNodes
		latestRule.Status.ObservedGeneration = rule.Status.ObservedGeneration
		latestRule.Status.DryRunResults = rule.Status.DryRunResults

		if err := r.Status().Patch(ctx, latestRule, patch); err != nil {
			log.V(1).Info("Status patch conflict, will retry",
				"rule", rule.Name,
				"error", err.Error())
			return err
		}

		log.V(1).Info("Successfully patched rule status", "rule", rule.Name)
		return nil
	})
}

// processDryRun processes dry run for a rule.
//
//nolint:unparam // Keep error return for future extensibility and API stability.
func (r *RuleReadinessController) processDryRun(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, nodeList *corev1.NodeList) error {
	var affectedNodes, taintsToAdd, taintsToRemove, riskyOps int32
	var summaryParts []string

	for _, node := range nodeList.Items {
		if !r.ruleAppliesTo(ctx, rule, &node) {
			continue
		}

		affectedNodes++

		// Simulate rule evaluation using the rule's conditionPolicy
		conditionPolicy := rule.Spec.GetConditionPolicy()
		missingConditions := 0
		allSatisfied := true
		anySatisfied := false

		for _, condReq := range rule.Spec.Conditions {
			currentStatus, conditionFound := r.getConditionStatus(
				&node,
				condReq.Type,
				condReq.GetDefaultStatus(),
			)
			if !conditionFound {
				missingConditions++
			}
			if currentStatus != condReq.RequiredStatus {
				allSatisfied = false
			} else {
				anySatisfied = true
			}
		}

		shouldRemoveTaint := allSatisfied
		if conditionPolicy == readinessv1alpha1.ConditionPolicyAnyOf {
			shouldRemoveTaint = anySatisfied
		}
		currentlyHasTaint := r.hasTaintBySpec(&node, rule.Spec.Taint)

		if shouldRemoveTaint && currentlyHasTaint {
			taintsToRemove++
		} else if !shouldRemoveTaint && !currentlyHasTaint {
			taintsToAdd++
		}

		if missingConditions > 0 {
			riskyOps++
		}
	}

	// Build summary
	if taintsToAdd > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("would add %d taints", taintsToAdd))
	}
	if taintsToRemove > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("would remove %d taints", taintsToRemove))
	}
	if riskyOps > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d nodes have missing conditions", riskyOps))
	}

	summary := "No changes needed"
	if len(summaryParts) > 0 {
		summary = strings.Join(summaryParts, ", ")
	}

	// Update rule status with dry run results
	rule.Status.ObservedGeneration = rule.Generation
	rule.Status.DryRunResults = readinessv1alpha1.DryRunResults{
		AffectedNodes:   &affectedNodes,
		TaintsToAdd:     &taintsToAdd,
		TaintsToRemove:  &taintsToRemove,
		RiskyOperations: &riskyOps,
		Summary:         summary,
	}
	return nil
}

// cleanupTaintsForRule removes taints managed by this rule from all applicable nodes.
func (r *RuleReadinessController) cleanupTaintsForRule(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, nodeList *corev1.NodeList) error {
	log := ctrl.LoggerFrom(ctx)

	var errors []string
	for _, node := range nodeList.Items {
		applies, held := r.ruleAppliesToWithTaint(ctx, rule, &node)
		if !applies {
			continue
		}

		// Check if node has the taint managed by this rule
		if held {
			log.Info("Removing taint from node during rule cleanup",
				"node", node.Name,
				"rule", rule.Name,
				"taint", rule.Spec.Taint.Key)

			if err := r.removeTaintBySpec(ctx, &node, rule.Spec.Taint, rule.Name); err != nil {
				errors = append(errors, fmt.Sprintf("node %s: %v", node.Name, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to cleanup taints on some nodes: %s", strings.Join(errors, "; "))
	}

	return nil
}

func (r *RuleReconciler) ensureFinalizer(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, finalizer string) (finalizerAdded bool, err error) {
	// Finalizers can only be added when the deletionTimestamp is not set.
	if !rule.GetDeletionTimestamp().IsZero() {
		return false, nil
	}
	if controllerutil.ContainsFinalizer(rule, finalizer) {
		return false, nil
	}

	patch := client.MergeFrom(rule.DeepCopy())
	controllerutil.AddFinalizer(rule, finalizer)
	err = r.Patch(ctx, rule, patch)
	if err != nil {
		return false, err
	}
	return true, nil
}

// getPreviousNodeEvaluation retrieves the previous evaluation result for a specific node from the rule status.
// It returns nil (if the node is evaluated for the first time) otherwsie, return the previously evaluated node data.
func (r *RuleReadinessController) getPreviousNodeEvaluation(rule *readinessv1alpha1.NodeReadinessRule, nodeName string) *readinessv1alpha1.NodeEvaluation {
	for i := range rule.Status.NodeEvaluations {
		if rule.Status.NodeEvaluations[i].NodeName == nodeName {
			return &rule.Status.NodeEvaluations[i]
		}
	}
	return nil
}
