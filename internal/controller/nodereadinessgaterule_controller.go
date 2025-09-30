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
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	readinessv1alpha1 "github.com/ajaysundark/node-readiness-gate-controller/api/v1alpha1"
)

// ReadinessGateController manages node taints based on readiness gate rules
type ReadinessGateController struct {
	client.Client
	Scheme    *runtime.Scheme
	clientset kubernetes.Interface

	// Cache for efficient rule lookup
	ruleCacheMutex sync.RWMutex
	ruleCache      map[string]*readinessv1alpha1.NodeReadinessGateRule // ruleName -> rule

	workqueue workqueue.TypedRateLimitingInterface[string]

	nodeLister  corelisters.NodeLister
	nodesSynced cache.InformerSynced

	// Global dry run mode (emergency off-switch)
	globalDryRun bool
}

// NewReadinessGateController creates a new controller
func NewReadinessGateController(mgr ctrl.Manager, clientset kubernetes.Interface) *ReadinessGateController {
	return &ReadinessGateController{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		clientset: clientset,
		ruleCache: make(map[string]*readinessv1alpha1.NodeReadinessGateRule),
	}
}

// RuleReconciler handles NodeReadinessGateRule reconciliation
type RuleReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Controller *ReadinessGateController
}

// +kubebuilder:rbac:groups=nodereadiness.io,resources=nodereadinessgaterules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nodereadiness.io,resources=nodereadinessgaterules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nodereadiness.io,resources=nodereadinessgaterules/finalizers,verbs=update

func (r *RuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the rule
	rule := &readinessv1alpha1.NodeReadinessGateRule{}
	if err := r.Get(ctx, req.NamespacedName, rule); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Rule deleted, remove from cache and clean up
			r.Controller.removeRuleFromCache(req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Update rule cache
	r.Controller.updateRuleCache(rule)

	// Handle dry run
	if rule.Spec.DryRun {
		if err := r.Controller.processDryRun(ctx, rule); err != nil {
			log.Error(err, "Failed to process dry run", "rule", rule.Name)
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
	} else {
		// Clear previous dry run results
		rule.Status.DryRunResults = nil

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

	return ctrl.Result{}, nil
}

// processAllNodesForRule processes all nodes when a rule changes
func (r *ReadinessGateController) processAllNodesForRule(ctx context.Context, rule *readinessv1alpha1.NodeReadinessGateRule) error {
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return err
	}

	for _, node := range nodeList.Items {
		if r.ruleAppliesTo(rule, &node) {
			if err := r.evaluateRuleForNode(ctx, rule, &node); err != nil {
				// Log error but continue with other nodes
				r.recordNodeFailure(rule, node.Name, "EvaluationError", err.Error())
			}
		}
	}

	return nil
}

// evaluateRuleForNode evaluates a single rule against a single node
func (r *ReadinessGateController) evaluateRuleForNode(ctx context.Context, rule *readinessv1alpha1.NodeReadinessGateRule, node *corev1.Node) error {
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
	}

	// Determine taint action
	shouldRemoveTaint := allConditionsSatisfied
	currentlyHasTaint := r.hasTaintBySpec(node, rule.Spec.Taint)

	var action string
	var err error

	if shouldRemoveTaint && currentlyHasTaint {
		action = "remove"
		log.Info("Removing taint", "node", node.Name, "rule", rule.Name, "taint", rule.Spec.Taint.Key)

		if err = r.removeTaintBySpec(ctx, node, rule.Spec.Taint); err != nil {
			return fmt.Errorf("failed to remove taint: %w", err)
		}

		// Mark bootstrap completed if bootstrap-only mode
		if rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly {
			r.markBootstrapCompleted(ctx, node.Name, rule.Name)
		}

	} else if !shouldRemoveTaint && !currentlyHasTaint {
		action = "add"
		log.Info("Adding taint", "node", node.Name, "rule", rule.Name, "taint", rule.Spec.Taint.Key)

		if err = r.addTaintBySpec(ctx, node, rule.Spec.Taint); err != nil {
			return fmt.Errorf("failed to add taint: %w", err)
		}
	} else {
		action = "none"
		log.V(4).Info("No taint action needed", "node", node.Name, "rule", rule.Name)
	}

	// Update evaluation status
	r.updateNodeEvaluationStatus(rule, node.Name, conditionResults, action)

	return nil
}

// getApplicableRulesForNode returns all rules applicable to a node
func (r *ReadinessGateController) getApplicableRulesForNode(node *corev1.Node) []*readinessv1alpha1.NodeReadinessGateRule {
	r.ruleCacheMutex.RLock()
	defer r.ruleCacheMutex.RUnlock()

	var applicableRules []*readinessv1alpha1.NodeReadinessGateRule

	for _, rule := range r.ruleCache {
		if r.ruleAppliesTo(rule, node) {
			applicableRules = append(applicableRules, rule)
		}
	}

	return applicableRules
}

// ruleAppliesTo checks if a rule applies to a node
func (r *ReadinessGateController) ruleAppliesTo(rule *readinessv1alpha1.NodeReadinessGateRule, node *corev1.Node) bool {
	if rule.Spec.NodeSelector == nil {
		return true
	}

	selector, err := metav1.LabelSelectorAsSelector(rule.Spec.NodeSelector)
	if err != nil {
		klog.Errorf("Invalid node selector for rule %s: %v", rule.Name, err)
		return false
	}

	return selector.Matches(labels.Set(node.Labels))
}

// updateRuleCache updates the rule cache
func (r *ReadinessGateController) updateRuleCache(rule *readinessv1alpha1.NodeReadinessGateRule) {
	r.ruleCacheMutex.Lock()
	defer r.ruleCacheMutex.Unlock()

	r.ruleCache[rule.Name] = rule.DeepCopy()
}

// removeRuleFromCache removes a rule from cache
func (r *ReadinessGateController) removeRuleFromCache(ruleName string) {
	r.ruleCacheMutex.Lock()
	defer r.ruleCacheMutex.Unlock()

	delete(r.ruleCache, ruleName)
}

// updateRuleStatus updates the status of a NodeReadinessGateRule
func (r *ReadinessGateController) updateRuleStatus(ctx context.Context, rule *readinessv1alpha1.NodeReadinessGateRule) error {
	// Get nodes that this rule applies to
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return err
	}

	var appliedNodes []string
	var completedNodes []string

	for _, node := range nodeList.Items {
		if r.ruleAppliesTo(rule, &node) {
			appliedNodes = append(appliedNodes, node.Name)

			// Check if bootstrap completed for bootstrap-only rules
			if rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly {
				if r.isBootstrapCompleted(node.Name, rule.Name) {
					completedNodes = append(completedNodes, node.Name)
				}
			}
		}
	}

	// Update status
	rule.Status.ObservedGeneration = rule.Generation
	rule.Status.AppliedNodes = appliedNodes
	rule.Status.CompletedNodes = completedNodes

	return r.Status().Update(ctx, rule)
}

// processDryRun processes dry run for a rule
func (r *ReadinessGateController) processDryRun(ctx context.Context, rule *readinessv1alpha1.NodeReadinessGateRule) error {
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return err
	}

	var affectedNodes, taintsToAdd, taintsToRemove, riskyOps int
	var summaryParts []string

	for _, node := range nodeList.Items {
		if !r.ruleAppliesTo(rule, &node) {
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
	rule.Status.DryRunResults = &readinessv1alpha1.DryRunResults{
		AffectedNodes:   affectedNodes,
		TaintsToAdd:     taintsToAdd,
		TaintsToRemove:  taintsToRemove,
		RiskyOperations: riskyOps,
		Summary:         summary,
	}

	return nil
}

// SetGlobalDryRun sets the global dry run mode (emergency off-switch)
func (r *ReadinessGateController) SetGlobalDryRun(dryRun bool) {
	r.globalDryRun = dryRun
}

// SetupWithManager sets up the controller with the Manager.
func (r *RuleReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	c, err := controller.New("nodereadiness-controller", mgr, controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: 1,
	})
	if err != nil {
		return err
	}

	// Watch for rule changes to reprocess all nodes
	mapFn := func(ctx context.Context, rule *readinessv1alpha1.NodeReadinessGateRule) []reconcile.Request {
		log := ctrl.LoggerFrom(ctx)
		opts := []client.ListOption{}
		if rule.Spec.NodeSelector != nil {
			selector, err := metav1.LabelSelectorAsSelector(rule.Spec.NodeSelector)
			if err == nil {
				opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
			}
		}

		log.Info("Rule changed, reprocessing applicable nodes", "rule", rule.Name)
		nodeList := &corev1.NodeList{}
		if err := r.List(context.Background(), nodeList, opts...); err != nil {
			log.Error(err, "Failed to list nodes for rule", "rule", rule.Name)
			return nil
		}

		var requests []reconcile.Request
		for _, node := range nodeList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKey{Name: node.Name},
			})
		}
		return requests
	}

	// Custom predicate to handle both CREATE and UPDATE events
	rulePredicate := predicate.TypedFuncs[*readinessv1alpha1.NodeReadinessGateRule]{
		CreateFunc: func(e event.TypedCreateEvent[*readinessv1alpha1.NodeReadinessGateRule]) bool {
			log := ctrl.LoggerFrom(ctx)
			log.Info("Processing rule create event - evaluating existing nodes", "rule", e.Object.Name)
			return true
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*readinessv1alpha1.NodeReadinessGateRule]) bool {
			log := ctrl.LoggerFrom(ctx)
			// Only trigger if generation changed (spec changed)
			if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
				log.Info("Processing rule update event - spec changed", "rule", e.ObjectNew.Name)
				return true
			}
			return false
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*readinessv1alpha1.NodeReadinessGateRule]) bool {
			log := ctrl.LoggerFrom(ctx)
			log.Info("Processing rule delete event", "rule", e.Object.Name)
			return false // No need to reconcile on delete
		},
	}

	err = c.Watch(
		source.Kind(mgr.GetCache(), &readinessv1alpha1.NodeReadinessGateRule{},
			handler.TypedEnqueueRequestsFromMapFunc[*readinessv1alpha1.NodeReadinessGateRule](mapFn),
			rulePredicate))
	if err != nil {
		return err
	}

	// Watch for node additions and trigger reconciliation for all rules
	mapNodeToRules := func(ctx context.Context, node *corev1.Node) []reconcile.Request {
		log := ctrl.LoggerFrom(ctx)
		ruleList := &readinessv1alpha1.NodeReadinessGateRuleList{}
		if err := r.List(ctx, ruleList); err != nil {
			log.Error(err, "Failed to list rules for node", "node", node.Name)
			return nil
		}

		log.Info("Node changed, reprocessing all rules", "node", node.Name, "ruleCount", len(ruleList.Items))
		requests := make([]reconcile.Request, len(ruleList.Items))
		for i, rule := range ruleList.Items {
			requests[i] = reconcile.Request{
				NamespacedName: client.ObjectKey{Name: rule.Name},
			}
		}
		return requests
	}

	nodePredicate := predicate.TypedFuncs[*corev1.Node]{
		CreateFunc: func(e event.TypedCreateEvent[*corev1.Node]) bool {
			log := ctrl.LoggerFrom(ctx)
			log.Info("Processing node create event", "node", e.Object.Name)
			return true
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*corev1.Node]) bool {
			log := ctrl.LoggerFrom(ctx)
			log.Info("Processing node update event", "node", e.ObjectNew.Name)
			return true
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*corev1.Node]) bool {
			log := ctrl.LoggerFrom(ctx)
			log.Info("Processing node delete event", "node", e.Object.Name)
			return false
		},
	}

	err = c.Watch(
		source.Kind(mgr.GetCache(), &corev1.Node{},
			handler.TypedEnqueueRequestsFromMapFunc[*corev1.Node](mapNodeToRules),
			nodePredicate))
	if err != nil {
		return err
	}

	return nil
}
