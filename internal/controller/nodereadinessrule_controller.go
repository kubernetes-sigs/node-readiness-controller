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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

const (
	// finalizerName is the finalizer added to NodeReadinessRule to ensure cleanup.
	finalizerName = "readiness.node.x-k8s.io/cleanup-taints"
)

// ReadinessGateController manages node taints based on readiness gate rules.
type ReadinessGateController struct {
	client.Client
	Scheme    *runtime.Scheme
	clientset kubernetes.Interface

	// Cache for efficient rule lookup
	ruleCacheMutex sync.RWMutex
	ruleCache      map[string]*readinessv1alpha1.NodeReadinessRule // ruleName -> rule

	// Global dry run mode (emergency off-switch)
	globalDryRun bool
}

// NewReadinessGateController creates a new controller.
func NewReadinessGateController(mgr ctrl.Manager, clientset kubernetes.Interface) *ReadinessGateController {
	return &ReadinessGateController{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		clientset: clientset,
		ruleCache: make(map[string]*readinessv1alpha1.NodeReadinessRule),
	}
}

// RuleReconciler handles NodeReadinessRule reconciliation.
type RuleReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Controller *ReadinessGateController
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
		if client.IgnoreNotFound(err) == nil {
			// Rule deleted, remove from cache
			log.Info("Rule not found, removing from cache", "rule", req.Name)
			r.Controller.removeRuleFromCache(ctx, req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion with finalizer
	if rule.DeletionTimestamp != nil {
		if containsFinalizer(rule, finalizerName) {
			// Rule is being deleted, clean up taints before removing finalizer
			log.Info("Cleaning up taints for deleted rule", "rule", rule.Name)
			if err := r.Controller.cleanupTaintsForRule(ctx, rule); err != nil {
				log.Error(err, "Failed to cleanup taints for rule", "rule", rule.Name)
				return ctrl.Result{RequeueAfter: time.Minute}, err
			}

			// Remove finalizer
			removeFinalizer(rule, finalizerName)
			if err := r.Update(ctx, rule); err != nil {
				log.Error(err, "Failed to remove finalizer", "rule", rule.Name)
				return ctrl.Result{}, err
			}

			// Remove from cache after successful cleanup
			r.Controller.removeRuleFromCache(ctx, rule.Name)
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	finalizerAdded := false
	if !containsFinalizer(rule, finalizerName) {
		addFinalizer(rule, finalizerName)
		if err := r.Update(ctx, rule); err != nil {
			log.Error(err, "Failed to add finalizer", "rule", rule.Name)
			return ctrl.Result{}, err
		}
		finalizerAdded = true

		// Refresh rule after update to get latest version
		if err := r.Get(ctx, req.NamespacedName, rule); err != nil {
			return ctrl.Result{}, err
		}
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

	if finalizerAdded {
		log.V(4).Info("Finalizer added to rule", "rule", rule.Name)
	}

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

// cleanupDeletedNodes removes status entries for nodes that no longer exist.
func (r *ReadinessGateController) cleanupDeletedNodes(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) error {
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

		fresh.Status.NodeEvaluations = freshNodeEvaluations
		return r.Status().Update(ctx, fresh)
	})
}

// processAllNodesForRule processes all nodes when a rule changes.
func (r *ReadinessGateController) processAllNodesForRule(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) error {
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
func (r *ReadinessGateController) evaluateRuleForNode(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, node *corev1.Node) error {
	log := ctrl.LoggerFrom(ctx)

	// Evaluate all conditions (ALL logic)
	allConditionsSatisfied := true
	conditionResults := make([]readinessv1alpha1.ConditionEvaluationResult, 0, len(rule.Spec.Conditions))

	for _, condReq := range rule.Spec.Conditions {
		currentStatus := r.getConditionStatus(node, condReq.Type)
		satisfied := currentStatus == condReq.RequiredStatus
		missing := currentStatus == corev1.ConditionUnknown

		if !satisfied {
			allConditionsSatisfied = false
		}

		conditionResults = append(conditionResults, readinessv1alpha1.ConditionEvaluationResult{
			Type:           condReq.Type,
			CurrentStatus:  currentStatus,
			RequiredStatus: condReq.RequiredStatus,
			Satisfied:      satisfied,
			Missing:        missing,
		})

		log.V(1).Info("Condition evaluation", "node", node.Name, "rule", rule.Name,
			"conditionType", condReq.Type, "current", currentStatus, "required", condReq.RequiredStatus,
			"satisfied", satisfied, "missing", missing)
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
			return fmt.Errorf("failed to remove taint: %w", err)
		}

		// Mark bootstrap completed if bootstrap-only mode
		if rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly {
			r.markBootstrapCompleted(ctx, node.Name, rule.Name)
		}

	case !shouldRemoveTaint && !currentlyHasTaint:
		log.Info("Adding taint", "node", node.Name, "rule", rule.Name, "taint", rule.Spec.Taint.Key)

		if err = r.addTaintBySpec(ctx, node, rule.Spec.Taint); err != nil {
			return fmt.Errorf("failed to add taint: %w", err)
		}

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
func (r *ReadinessGateController) updateNodeEvaluationStatus(
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
func (r *ReadinessGateController) getApplicableRulesForNode(ctx context.Context, node *corev1.Node) []*readinessv1alpha1.NodeReadinessRule {
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
func (r *ReadinessGateController) ruleAppliesTo(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, node *corev1.Node) bool {
	log := ctrl.LoggerFrom(ctx)

	selector, err := metav1.LabelSelectorAsSelector(&rule.Spec.NodeSelector)
	if err != nil {
		log.Error(err, "Invalid node selector for rule", "rule", rule.Name)
		return false
	}

	return selector.Matches(labels.Set(node.Labels))
}

// updateRuleCache updates the rule cache.
func (r *ReadinessGateController) updateRuleCache(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) {
	log := ctrl.LoggerFrom(ctx)
	r.ruleCacheMutex.Lock()
	defer r.ruleCacheMutex.Unlock()

	ruleCopy := rule.DeepCopy()
	r.ruleCache[rule.Name] = ruleCopy
	log.V(4).Info("Updated rule cache",
		"rule", rule.Name,
		"totalRules", len(r.ruleCache),
		"resourceVersion", ruleCopy.ResourceVersion)
}

// getCachedRule retrieves a rule from cache.
func (r *ReadinessGateController) getCachedRule(ruleName string) *readinessv1alpha1.NodeReadinessRule {
	r.ruleCacheMutex.RLock()
	defer r.ruleCacheMutex.RUnlock()

	rule, exists := r.ruleCache[ruleName]
	if !exists {
		return nil
	}
	return rule.DeepCopy()
}

// removeRuleFromCache removes a rule from cache.
func (r *ReadinessGateController) removeRuleFromCache(ctx context.Context, ruleName string) {
	log := ctrl.LoggerFrom(ctx)
	r.ruleCacheMutex.Lock()
	defer r.ruleCacheMutex.Unlock()

	delete(r.ruleCache, ruleName)
	log.Info("Removed rule from cache", "rule", ruleName, "totalRules", len(r.ruleCache))
}

// updateRuleStatus updates the status of a NodeReadinessRule.
func (r *ReadinessGateController) updateRuleStatus(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) error {
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

		// Merge our status updates into fresh version
		// This ensures we're updating based on the latest resourceVersion
		latestRule.Status.NodeEvaluations = rule.Status.NodeEvaluations
		latestRule.Status.AppliedNodes = rule.Status.AppliedNodes
		latestRule.Status.FailedNodes = rule.Status.FailedNodes
		latestRule.Status.ObservedGeneration = rule.Status.ObservedGeneration
		latestRule.Status.DryRunResults = rule.Status.DryRunResults

		if err := r.Status().Update(ctx, latestRule); err != nil {
			log.V(1).Info("Status update conflict, will retry",
				"rule", rule.Name,
				"error", err.Error())
			return err
		}

		log.V(1).Info("Successfully updated rule status", "rule", rule.Name)
		return nil
	})
}

// processDryRun processes dry run for a rule.
func (r *ReadinessGateController) processDryRun(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) error {
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

// SetGlobalDryRun sets the global dry run mode (emergency off-switch).
func (r *ReadinessGateController) SetGlobalDryRun(dryRun bool) {
	r.globalDryRun = dryRun
}

// cleanupTaintsForRule removes taints managed by this rule from all applicable nodes.
func (r *ReadinessGateController) cleanupTaintsForRule(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) error {
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
func (r *ReadinessGateController) cleanupNodesAfterSelectorChange(ctx context.Context, oldRule, newRule *readinessv1alpha1.NodeReadinessRule) error {
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

// containsFinalizer checks if a finalizer exists in the rule.
func containsFinalizer(rule *readinessv1alpha1.NodeReadinessRule, finalizer string) bool {
	for _, f := range rule.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}

// addFinalizer adds a finalizer to the rule.
func addFinalizer(rule *readinessv1alpha1.NodeReadinessRule, finalizer string) {
	if !containsFinalizer(rule, finalizer) {
		rule.Finalizers = append(rule.Finalizers, finalizer)
	}
}

// removeFinalizer removes a finalizer from the rule.
func removeFinalizer(rule *readinessv1alpha1.NodeReadinessRule, finalizer string) {
	var newFinalizers []string
	for _, f := range rule.Finalizers {
		if f != finalizer {
			newFinalizers = append(newFinalizers, f)
		}
	}
	rule.Finalizers = newFinalizers
}

// SetupWithManager sets up the controller with the Manager.
func (r *RuleReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("nodereadiness-controller").
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		For(&readinessv1alpha1.NodeReadinessRule{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
