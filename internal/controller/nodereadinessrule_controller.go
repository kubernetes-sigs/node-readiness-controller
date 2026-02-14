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
	Scheme    *runtime.Scheme
	clientset kubernetes.Interface

	// Cache for efficient rule lookup
	ruleCacheMutex sync.RWMutex
	ruleCache      map[string]*readinessv1alpha1.NodeReadinessRule // ruleName -> rule
}

// RuleReconciler handles NodeReadinessRule reconciliation.
type RuleReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Controller *RuleReadinessController
}

// NewRuleReadinessController creates a new controller.
func NewRuleReadinessController(mgr ctrl.Manager, clientset kubernetes.Interface) *RuleReadinessController {
	return &RuleReadinessController{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		clientset: clientset,
		ruleCache: make(map[string]*readinessv1alpha1.NodeReadinessRule),
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *RuleReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("nodereadiness-controller").
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		For(&readinessv1alpha1.NodeReadinessRule{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrules/finalizers,verbs=update

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

	// Handle deletion reconciliation loop.
	if !rule.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, rule)
	}

	// Detect nodeSelector changes and cleanup old nodes
	cachedRule := r.Controller.getCachedRule(rule.Name)
	if cachedRule != nil && nodeSelectorChanged(rule.Spec.NodeSelector, cachedRule.Spec.NodeSelector) {
		log.Info("NodeSelector changed, cleaning up nodes from old selector", "rule", rule.Name)
		if err := r.Controller.cleanupNodesAfterSelectorChange(ctx, cachedRule, rule); err != nil {
			log.Error(err, "Failed to cleanup nodes after selector change", "rule", rule.Name)
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
	}

	// Update rule cache (after cleanup)
	r.Controller.updateRuleCache(ctx, rule)

	// Handle dry run
	if rule.Spec.DryRun {
		if err := r.Controller.processDryRun(ctx, rule); err != nil {
			log.Error(err, "Failed to process dry run", "rule", rule.Name)
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
	} else {
		// Clear previous dry run results
		rule.Status.DryRunResults = readinessv1alpha1.DryRunResults{}

		// Process all applicable nodes for this rule
		if err := r.Controller.processAllNodesForRule(ctx, rule); err != nil {
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
	if err := r.Controller.cleanupDeletedNodes(ctx, rule); err != nil {
		log.Error(err, "Failed to clean up deleted nodes", "rule", rule.Name)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	return ctrl.Result{}, nil
}

// reconcileDelete handles the rules deletion, It performs following actions
// 1. Deletes the taints associated with the rule.
// 2. Remove the rule from the cache.
// 3. Remove the finalizer from the rule.
func (r *RuleReconciler) reconcileDelete(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Update cache with deletion-marked rule before cleanup.
	log.V(3).Info("Updating cache with deletion-marked rule before cleanup")
	r.Controller.updateRuleCache(ctx, rule)

	log.Info("Cleaning up taints for deleted rule", "rule", rule.Name)
	if err := r.Controller.cleanupTaintsForRule(ctx, rule); err != nil {
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
	return ctrl.Result{}, nil
}

// cleanupDeletedNodes removes status entries for nodes that no longer exist.
func (r *RuleReadinessController) cleanupDeletedNodes(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) error {
	log := ctrl.LoggerFrom(ctx)

	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return err
	}

	existingNodes := make(map[string]bool)
	for _, node := range nodeList.Items {
		existingNodes[node.Name] = true
	}

	// Filter out deleted nodes
	var newNodeEvaluations []readinessv1alpha1.NodeEvaluation
	for _, evaluation := range rule.Status.NodeEvaluations {
		if existingNodes[evaluation.NodeName] {
			newNodeEvaluations = append(newNodeEvaluations, evaluation)
		}
	}

	if len(newNodeEvaluations) == len(rule.Status.NodeEvaluations) {
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

		var freshNodeEvaluations []readinessv1alpha1.NodeEvaluation
		for _, evaluation := range fresh.Status.NodeEvaluations {
			if existingNodes[evaluation.NodeName] {
				freshNodeEvaluations = append(freshNodeEvaluations, evaluation)
			}
		}

		if len(freshNodeEvaluations) == len(fresh.Status.NodeEvaluations) {
			return nil
		}

		patch := client.MergeFrom(fresh.DeepCopy())
		fresh.Status.NodeEvaluations = freshNodeEvaluations
		return r.Status().Patch(ctx, fresh, patch)
	})
}

// processAllNodesForRule processes all nodes when a rule changes.
func (r *RuleReadinessController) processAllNodesForRule(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) error {
	log := ctrl.LoggerFrom(ctx)

	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return err
	}

	log.Info("Processing all nodes for rule", "rule", rule.Name, "totalNodes", len(nodeList.Items))

	var appliedNodes []string
	for _, node := range nodeList.Items {
		if r.ruleAppliesTo(ctx, rule, &node) {
			appliedNodes = append(appliedNodes, node.Name)
			log.Info("Processing node for rule", "rule", rule.Name, "node", node.Name)
			if err := r.evaluateRuleForNode(ctx, rule, &node); err != nil {
				// Log error but continue with other nodes
				log.Error(err, "Failed to evaluate node for rule", "rule", rule.Name, "node", node.Name)
				r.recordNodeFailure(rule, node.Name, "EvaluationError", err.Error())
				metrics.Failures.WithLabelValues(rule.Name, "EvaluationError").Inc()
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
	timer := prometheus.NewTimer(metrics.EvaluationDuration)
	defer timer.ObserveDuration()
	log := ctrl.LoggerFrom(ctx)

	// Evaluate all conditions (ALL logic)
	allConditionsSatisfied := true
	conditionResults := make([]readinessv1alpha1.ConditionEvaluationResult, 0, len(rule.Spec.Conditions))

	for _, condReq := range rule.Spec.Conditions {
		currentStatus := r.getConditionStatus(node, condReq.Type)
		satisfied := currentStatus == condReq.RequiredStatus

		if !satisfied {
			allConditionsSatisfied = false
		}

		conditionResults = append(conditionResults, readinessv1alpha1.ConditionEvaluationResult{
			Type:           condReq.Type,
			CurrentStatus:  currentStatus,
			RequiredStatus: condReq.RequiredStatus,
		})

		log.V(1).Info("Condition evaluation", "node", node.Name, "rule", rule.Name,
			"conditionType", condReq.Type, "current", currentStatus, "required", condReq.RequiredStatus,
			"satisfied", satisfied)
	}

	// Determine taint action
	shouldRemoveTaint := allConditionsSatisfied
	currentlyHasTaint := r.hasTaintBySpec(node, rule.Spec.Taint)

	log.Info("Evaluation result", "node", node.Name, "rule", rule.Name,
		"allConditionsSatisfied", allConditionsSatisfied, "hasTaint", currentlyHasTaint)

	var err error

	switch {
	case shouldRemoveTaint && currentlyHasTaint:
		log.Info("Removing taint", "node", node.Name, "rule", rule.Name, "taint", rule.Spec.Taint.Key)

		if err = r.removeTaintBySpec(ctx, node, rule.Spec.Taint); err != nil {
			metrics.Failures.WithLabelValues(rule.Name, "RemoveTaintError").Inc()
			return fmt.Errorf("failed to remove taint: %w", err)
		}
		metrics.TaintOperations.WithLabelValues(rule.Name, "remove").Inc()

		// Mark bootstrap completed if bootstrap-only mode
		if rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly {
			r.markBootstrapCompleted(ctx, node.Name, rule.Name)
		}

	case !shouldRemoveTaint && !currentlyHasTaint:
		log.Info("Adding taint", "node", node.Name, "rule", rule.Name, "taint", rule.Spec.Taint.Key)

		if err = r.addTaintBySpec(ctx, node, rule.Spec.Taint); err != nil {
			metrics.Failures.WithLabelValues(rule.Name, "AddTaintError").Inc()
			return fmt.Errorf("failed to add taint: %w", err)
		}
		metrics.TaintOperations.WithLabelValues(rule.Name, "add").Inc()

	default:
		log.Info("No taint action needed", "node", node.Name, "rule", rule.Name,
			"shouldRemove", shouldRemoveTaint, "hasTaint", currentlyHasTaint)
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
			applicableRules = append(applicableRules, rule)
		}
	}

	return applicableRules
}

// ruleAppliesTo checks if a rule applies to a node.
func (r *RuleReadinessController) ruleAppliesTo(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, node *corev1.Node) bool {
	log := ctrl.LoggerFrom(ctx)

	selector, err := metav1.LabelSelectorAsSelector(&rule.Spec.NodeSelector)
	if err != nil {
		log.Error(err, "Invalid node selector for rule", "rule", rule.Name)
		return false
	}

	return selector.Matches(labels.Set(node.Labels))
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

// getCachedRule retrieves a rule from cache.
func (r *RuleReadinessController) getCachedRule(ruleName string) *readinessv1alpha1.NodeReadinessRule {
	r.ruleCacheMutex.RLock()
	defer r.ruleCacheMutex.RUnlock()

	rule, exists := r.ruleCache[ruleName]
	if !exists {
		return nil
	}
	return rule.DeepCopy()
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
func (r *RuleReadinessController) processDryRun(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) error {
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return err
	}

	var affectedNodes, taintsToAdd, taintsToRemove, riskyOps int32
	var summaryParts []string

	for _, node := range nodeList.Items {
		if !r.ruleAppliesTo(ctx, rule, &node) {
			continue
		}

		affectedNodes++

		// Simulate rule evaluation
		allConditionsSatisfied := true
		missingConditions := 0

		for _, condReq := range rule.Spec.Conditions {
			currentStatus := r.getConditionStatus(&node, condReq.Type)
			if currentStatus == corev1.ConditionUnknown {
				missingConditions++
			}
			if currentStatus != condReq.RequiredStatus {
				allConditionsSatisfied = false
			}
		}

		shouldRemoveTaint := allConditionsSatisfied
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
func (r *RuleReadinessController) cleanupTaintsForRule(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) error {
	log := ctrl.LoggerFrom(ctx)

	// Get all nodes that this rule applies to
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	var errors []string
	for _, node := range nodeList.Items {
		if !r.ruleAppliesTo(ctx, rule, &node) {
			continue
		}

		// Check if node has the taint managed by this rule
		if r.hasTaintBySpec(&node, rule.Spec.Taint) {
			log.Info("Removing taint from node during rule cleanup",
				"node", node.Name,
				"rule", rule.Name,
				"taint", rule.Spec.Taint.Key)

			if err := r.removeTaintBySpec(ctx, &node, rule.Spec.Taint); err != nil {
				errors = append(errors, fmt.Sprintf("node %s: %v", node.Name, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to cleanup taints on some nodes: %s", strings.Join(errors, "; "))
	}

	return nil
}

// cleanupNodesAfterSelectorChange cleans up nodes that matched old selector but not new one.
func (r *RuleReadinessController) cleanupNodesAfterSelectorChange(ctx context.Context, oldRule, newRule *readinessv1alpha1.NodeReadinessRule) error {
	log := ctrl.LoggerFrom(ctx)

	// Get all nodes
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	// Build old selector
	oldSelector, err := metav1.LabelSelectorAsSelector(&oldRule.Spec.NodeSelector)
	if err != nil {
		return fmt.Errorf("failed to parse old node selector: %w", err)
	}

	// Clean up nodes that matched old selector but not new selector
	var errors []string
	for _, node := range nodeList.Items {
		// Check if node matched old selector
		matchedOld := false
		if oldSelector == nil {
			// nil selector matches all nodes
			matchedOld = true
		} else {
			matchedOld = oldSelector.Matches(labels.Set(node.Labels))
		}

		// Check if node matches new selector (use newRule for current evaluation)
		matchesNew := r.ruleAppliesTo(ctx, newRule, &node)

		// If node matched old but not new, clean up the taint
		if matchedOld && !matchesNew {
			if r.hasTaintBySpec(&node, newRule.Spec.Taint) {
				log.Info("Removing taint from node that no longer matches selector",
					"node", node.Name,
					"rule", newRule.Name,
					"taint", newRule.Spec.Taint.Key)

				if err := r.removeTaintBySpec(ctx, &node, newRule.Spec.Taint); err != nil {
					errors = append(errors, fmt.Sprintf("node %s: %v", node.Name, err))
				}
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
