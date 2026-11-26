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
	"sort"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
	"sigs.k8s.io/node-readiness-controller/internal/metrics"
)

const (
	// finalizerName is the finalizer added to NodeReadinessRule to ensure cleanup.
	finalizerName = "readiness.node.x-k8s.io/cleanup-taints"
)

// statusDebounce coalesces bursts of node events into a single status reconcile per rule.
// It only delays status reporting; taint enforcement is done by NodeReconciler without delay.
const statusDebounce = time.Second

// deletionRecheckInterval is how often a deleting rule re-checks whether NodeReconciler
// has released all the taints the rule owned.
const deletionRecheckInterval = time.Second

// RuleReadinessController holds the state shared by RuleReconciler and NodeReconciler.
//
// Ownership:
//   - NodeReconciler is the only writer of Nodes (taints, bootstrap annotations) and
//     NodeReadinessEvaluations. It reacts to node and rule events.
//   - RuleReconciler is the only writer of NodeReadinessRules (finalizer, status). It
//     derives status from the observed node state and never writes to Nodes.
//
// Both read rules and nodes from the informer cache; no controller-local copy is kept.
type RuleReadinessController struct {
	client.Client
	Scheme                 *runtime.Scheme
	clientset              kubernetes.Interface
	EventRecorder          events.EventRecorder
	EnableNodeStateMetrics bool
	EnableNRE              bool
}

// RuleReconciler handles NodeReadinessRule reconciliation.
type RuleReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Controller              *RuleReadinessController
	MaxConcurrentReconciles int // caps how many rules are reconciled concurrently
}

// NewRuleReadinessController creates a new controller.
func NewRuleReadinessController(mgr ctrl.Manager, clientset kubernetes.Interface, enableNodeStateMetrics bool, enableNRE bool) *RuleReadinessController {
	return &RuleReadinessController{
		Client:                 mgr.GetClient(),
		Scheme:                 mgr.GetScheme(),
		clientset:              clientset,
		EventRecorder:          mgr.GetEventRecorder("node-readiness-controller"),
		EnableNodeStateMetrics: enableNodeStateMetrics,
		EnableNRE:              enableNRE,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *RuleReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	concurrency := max(r.MaxConcurrentReconciles, 1)
	return ctrl.NewControllerManagedBy(mgr).
		Named("nodereadiness-controller").
		WithOptions(controller.Options{MaxConcurrentReconciles: concurrency}).
		For(&readinessv1alpha1.NodeReadinessRule{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		// Node changes only refresh the rule status (and a deleting rule's wait for taint
		// release). They are debounced so N node events cost roughly one status patch per rule.
		Watches(&corev1.Node{}, r.nodeToRulesHandler(),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					oldNode, okOld := e.ObjectOld.(*corev1.Node)
					newNode, okNew := e.ObjectNew.(*corev1.Node)
					return okOld && okNew && nodeStatusRelevantChange(oldNode, newNode)
				},
			})).
		Complete(r)
}

// nodeToRulesHandler maps node events to the rules selecting that node, delayed by statusDebounce.
// The workqueue keeps the earliest due time for a repeated key, so a burst of events is coalesced.
func (r *RuleReconciler) nodeToRulesHandler() handler.EventHandler {
	enqueue := func(ctx context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request], nodes ...client.Object) {
		rules, err := r.Controller.listRulesReadOnly(ctx)
		if err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "Failed to list rules to map node event")
			return
		}
		for _, obj := range nodes {
			node, ok := obj.(*corev1.Node)
			if !ok || node == nil {
				continue
			}
			for _, rule := range rules {
				if !r.Controller.ruleAppliesTo(ctx, rule, node) {
					continue
				}
				// A rule matching both the old and new node is added twice; the queue coalesces it.
				q.AddAfter(reconcile.Request{NamespacedName: types.NamespacedName{Name: rule.Name}}, statusDebounce)
			}
		}
	}

	return handler.Funcs{
		CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, q, e.Object)
		},
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			// Include the old object so a rule that stops selecting the node is refreshed too.
			enqueue(ctx, q, e.ObjectNew, e.ObjectOld)
		},
		DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, q, e.Object)
		},
	}
}

// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrules,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrules/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

func (r *RuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.V(2).Info("Reconciling rule", "rule", req.Name)

	r.Controller.updateRulesTotal(ctx)

	// Fetch the rule
	rule := &readinessv1alpha1.NodeReadinessRule{}
	if err := r.Get(ctx, req.NamespacedName, rule); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(2).Info("Rule not found", "rule", req.Name)
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
		// NodeReconciler is notified of the finalizer by its own rule watch and starts enforcing meanwhile.
		log.V(3).Info("Finalizer added, requeuing")
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList, client.UnsafeDisableDeepCopy); err != nil {
		return ctrl.Result{}, err
	}

	// Handle deletion reconciliation loop.
	if !rule.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, rule, nodeList)
	}

	if rule.Spec.DryRun {
		if err := r.Controller.processDryRun(ctx, rule, nodeList); err != nil {
			log.Error(err, "Failed to process dry run", "rule", rule.Name)
			return ctrl.Result{}, err
		}
	} else {
		r.Controller.deriveRuleStatus(rule, nodeList)
	}

	// The whole status is derived from the observed node state and written in one patch.
	if err := r.Controller.updateRuleStatus(ctx, rule); err != nil {
		log.Error(err, "Failed to update rule status", "rule", rule.Name)
		return ctrl.Result{}, err
	}

	// Update top-level rule metrics.
	metrics.RuleLastReconciliationTime.WithLabelValues(rule.Name).Set(float64(time.Now().Unix()))

	if r.Controller.EnableNodeStateMetrics {
		r.Controller.SyncNodeStateMetrics(ctx, rule)
	}

	return ctrl.Result{}, nil
}

// reconcileDelete handles the rule deletion. NodeReconciler is the only writer of Nodes,
// so it removes the rule's taint from every node it selects (it watches the rule). This
// function only waits for that to finish before releasing the finalizer.
func (r *RuleReconciler) reconcileDelete(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, nodeList *corev1.NodeList) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	if holding := r.Controller.nodesHoldingRuleTaint(rule, nodeList); holding > 0 {
		log.V(2).Info("Waiting for node controller to release the rule's taints", "rule", rule.Name, "nodes", holding)
		return ctrl.Result{RequeueAfter: deletionRecheckInterval}, nil
	}

	log.V(3).Info("Removing the finalizer from the rule")
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &readinessv1alpha1.NodeReadinessRule{}
		if err := r.Get(ctx, client.ObjectKey{Name: rule.Name}, latest); err != nil {
			return client.IgnoreNotFound(err)
		}
		if !controllerutil.ContainsFinalizer(latest, finalizerName) {
			return nil
		}

		stored := latest.DeepCopy()
		controllerutil.RemoveFinalizer(latest, finalizerName)
		return r.Patch(ctx, latest, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{}))
	})
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
	//nolint:staticcheck
	metrics.ReconciliationLatency.DeletePartialMatch(ruleLabel)
	metrics.EnforcementLatency.DeletePartialMatch(ruleLabel)

	return ctrl.Result{}, nil
}

// listRulesReadOnly lists the rules from the informer cache without deep-copying them.
// The returned objects are shared with the cache and must not be modified.
func (r *RuleReadinessController) listRulesReadOnly(ctx context.Context) ([]*readinessv1alpha1.NodeReadinessRule, error) {
	ruleList := &readinessv1alpha1.NodeReadinessRuleList{}
	if err := r.List(ctx, ruleList, client.UnsafeDisableDeepCopy); err != nil {
		return nil, err
	}
	rules := make([]*readinessv1alpha1.NodeReadinessRule, len(ruleList.Items))
	for i := range ruleList.Items {
		rules[i] = &ruleList.Items[i]
	}
	return rules, nil
}

// getApplicableRulesForNode returns the rules selecting the node, read-only (see listRulesReadOnly).
func (r *RuleReadinessController) getApplicableRulesForNode(ctx context.Context, node *corev1.Node) []*readinessv1alpha1.NodeReadinessRule {
	rules, err := r.listRulesReadOnly(ctx)
	if err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "Failed to list rules", "node", node.Name)
		return nil
	}

	var applicable []*readinessv1alpha1.NodeReadinessRule
	for _, rule := range rules {
		if r.ruleAppliesTo(ctx, rule, node) {
			applicable = append(applicable, rule)
		}
	}
	return applicable
}

// updateRulesTotal refreshes the total number of rules metric.
func (r *RuleReadinessController) updateRulesTotal(ctx context.Context) {
	rules, err := r.listRulesReadOnly(ctx)
	if err != nil {
		return
	}
	metrics.RulesTotal.Set(float64(len(rules)))
}

// nodesHoldingRuleTaint counts the nodes selected by rule that still carry its taint.
func (r *RuleReadinessController) nodesHoldingRuleTaint(rule *readinessv1alpha1.NodeReadinessRule, nodeList *corev1.NodeList) int {
	selector, err := parseNodeSelector(rule)
	if err != nil {
		return 0
	}
	holding := 0
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if selector.Matches(labels.Set(node.Labels)) && r.hasTaintBySpec(node, rule.Spec.Taint) {
			holding++
		}
	}
	return holding
}

// deriveRuleStatus computes the per-node part of the rule status from the observed node
// state (conditions and taints) and sets it on rule. It is a pure function of the cached
// nodes plus the previous status, so it needs no information from NodeReconciler: the
// taint on a node is the result of enforcement.
//
// An entry is only re-timestamped when its content changes, so an unchanged rule produces
// an identical status and no API write.
func (r *RuleReadinessController) deriveRuleStatus(rule *readinessv1alpha1.NodeReadinessRule, nodeList *corev1.NodeList) {
	previous := make(map[string]readinessv1alpha1.NodeEvaluation, len(rule.Status.NodeEvaluations))
	for _, eval := range rule.Status.NodeEvaluations {
		previous[eval.NodeName] = eval
	}

	selector, err := parseNodeSelector(rule)
	if err != nil {
		return
	}

	bootstrapOnly := rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly
	applied := make([]string, 0, len(nodeList.Items))
	evaluations := make([]readinessv1alpha1.NodeEvaluation, 0, len(nodeList.Items))

	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if !selector.Matches(labels.Set(node.Labels)) {
			continue
		}
		applied = append(applied, node.Name)

		prev, hasPrev := previous[node.Name]

		// A completed bootstrap-only rule no longer monitors the node, so its last
		// evaluation is kept as is.
		if bootstrapOnly && hasPrev && nodeHasBootstrapAnnotation(node, rule) {
			evaluations = append(evaluations, prev)
			continue
		}

		outcomes, _ := r.evaluateRuleConditions(node, rule)
		results := make([]readinessv1alpha1.ConditionEvaluationResult, 0, len(outcomes))
		for _, outcome := range outcomes {
			results = append(results, outcome.result)
		}

		taintStatus := readinessv1alpha1.TaintStatusAbsent
		if r.hasTaintBySpec(node, rule.Spec.Taint) {
			taintStatus = readinessv1alpha1.TaintStatusPresent
		}

		eval := readinessv1alpha1.NodeEvaluation{
			NodeName:           node.Name,
			ConditionResults:   results,
			TaintStatus:        taintStatus,
			LastEvaluationTime: metav1.Now(),
		}
		if hasPrev && prev.TaintStatus == taintStatus && apiequality.Semantic.DeepEqual(prev.ConditionResults, results) {
			eval.LastEvaluationTime = prev.LastEvaluationTime
		}
		evaluations = append(evaluations, eval)
	}

	sort.Strings(applied)
	sort.Slice(evaluations, func(i, j int) bool { return evaluations[i].NodeName < evaluations[j].NodeName })

	rule.Status.ObservedGeneration = rule.Generation
	rule.Status.AppliedNodes = applied
	rule.Status.NodeEvaluations = evaluations
	// Per-node enforcement failures are reported by NodeReconciler through events,
	// metrics and retries; they can not be derived from node state.
	rule.Status.FailedNodes = nil
	rule.Status.DryRunResults = readinessv1alpha1.DryRunResults{}
}

// ListNodes returns the current list of Nodes.
func (r *RuleReadinessController) ListNodes(ctx context.Context) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return nil, err
	}
	return nodeList.Items, nil
}

// ListRules returns the current list of NodeReadinessRules.
func (r *RuleReadinessController) ListRules(ctx context.Context) ([]*readinessv1alpha1.NodeReadinessRule, error) {
	ruleList := &readinessv1alpha1.NodeReadinessRuleList{}
	if err := r.List(ctx, ruleList); err != nil {
		return nil, err
	}
	rules := make([]*readinessv1alpha1.NodeReadinessRule, len(ruleList.Items))
	for i := range ruleList.Items {
		rules[i] = &ruleList.Items[i]
	}
	return rules, nil
}

// forEachRuleNode applies callbacks to nodes matching each rule.
//
//nolint:unparam // keep error return for future extensibility and API stability.
func (r *RuleReadinessController) forEachRuleNode(
	ctx context.Context,
	nodes []corev1.Node,
	rules []*readinessv1alpha1.NodeReadinessRule,
	skipRule func(rule *readinessv1alpha1.NodeReadinessRule) bool,
	onRule func(rule *readinessv1alpha1.NodeReadinessRule),
	onNode func(rule *readinessv1alpha1.NodeReadinessRule, node *corev1.Node, held bool),
) error {
	log := ctrl.LoggerFrom(ctx)

	for _, rule := range rules {
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
func (r *RuleReadinessController) ListRuleNodeStates(ctx context.Context, nodes []corev1.Node, rules []*readinessv1alpha1.NodeReadinessRule) (map[string]metrics.RuleNodeCounts, error) {
	counts := make(map[string]metrics.RuleNodeCounts)

	err := r.forEachRuleNode(ctx, nodes, rules,
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
func (r *RuleReadinessController) ListBlockedNodes(ctx context.Context, nodes []corev1.Node, rules []*readinessv1alpha1.NodeReadinessRule) (map[string]metrics.RuleBlockedConditions, error) {
	result := make(map[string]metrics.RuleBlockedConditions)

	err := r.forEachRuleNode(ctx, nodes, rules,
		func(rule *readinessv1alpha1.NodeReadinessRule) bool { return false },
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

// ListRuleInventory counts rules by enforcement mode and dry-run state.
func (r *RuleReadinessController) ListRuleInventory(_ context.Context, rules []*readinessv1alpha1.NodeReadinessRule) (map[metrics.RuleModeKey]float64, error) {
	counts := make(map[metrics.RuleModeKey]float64)

	for _, rule := range rules {
		if !rule.DeletionTimestamp.IsZero() {
			continue
		}

		key := metrics.RuleModeKey{EnforcementMode: string(rule.Spec.EnforcementMode), DryRun: rule.Spec.DryRun}
		counts[key]++
	}

	return counts, nil
}

// ListRuleMatchedNodes returns the number of nodes matching each rule's NodeSelector.
func (r *RuleReadinessController) ListRuleMatchedNodes(ctx context.Context, nodes []corev1.Node, rules []*readinessv1alpha1.NodeReadinessRule) (map[string]float64, error) {
	log := ctrl.LoggerFrom(ctx)

	counts := make(map[string]float64, len(rules))
	for _, rule := range rules {
		// Parse the selector once per rule.
		selector, err := parseNodeSelector(rule)
		if err != nil {
			log.V(2).Info("Invalid node selector for rule", "rule", rule.Name, "error", err)
			continue
		}

		var matched float64
		for i := range nodes {
			if selector.Matches(labels.Set(nodes[i].Labels)) {
				matched++
			}
		}
		counts[rule.Name] = matched
	}

	return counts, nil
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

// patchRuleStatusWithOptimisticLock fetches the latest NodeReadinessRule, applies mutate to its
// status and patches the result to the API with an optimistic-locked JSON merge patch. The Patch
// call is skipped when mutate leaves the status unchanged.
//
// RuleReconciler is the only controller that writes NodeReadinessRule.Status, and the workqueue
// never runs two reconciles of the same rule at once. The optimistic lock is kept as a safety net
// against any other writer: a JSON merge patch replaces slice fields (NodeEvaluations,
// AppliedNodes, FailedNodes) wholesale rather than merging them, so without a resourceVersion
// precondition retry.RetryOnConflict can never observe a genuine conflict and a concurrent status
// write would be silently overwritten.
func (r *RuleReadinessController) patchRuleStatusWithOptimisticLock(
	ctx context.Context,
	ruleName string,
	mutate func(latest *readinessv1alpha1.NodeReadinessRule),
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latestRule := &readinessv1alpha1.NodeReadinessRule{}
		if err := r.Get(ctx, client.ObjectKey{Name: ruleName}, latestRule); err != nil {
			return err
		}

		stored := latestRule.DeepCopy()
		mutate(latestRule)

		if apiequality.Semantic.DeepEqual(stored.Status, latestRule.Status) {
			return nil
		}

		return r.Status().Patch(ctx, latestRule, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{}))
	})
}

// updateRuleStatus writes the status computed on rule with a single optimistic-locked patch.
func (r *RuleReadinessController) updateRuleStatus(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) error {
	desired := rule.Status.DeepCopy()
	return r.patchRuleStatusWithOptimisticLock(ctx, rule.Name, func(latest *readinessv1alpha1.NodeReadinessRule) {
		latest.Status = *desired
	})
}

// processDryRun processes dry run for a rule.
//
//nolint:unparam // Keep error return for future extensibility and API stability.
func (r *RuleReadinessController) processDryRun(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, nodeList *corev1.NodeList) error {
	var affectedNodes, taintsToAdd, taintsToRemove, riskyOps int32
	var summaryParts []string

	// An invalid selector matches no node. Parse it once rather than per node.
	selector, err := parseNodeSelector(rule)
	if err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "Invalid node selector for rule", "rule", rule.Name)
		selector = labels.Nothing()
	}

	// Index into the list: the nodes come from the informer cache without a deep copy.
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if !selector.Matches(labels.Set(node.Labels)) {
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
				node,
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
		currentlyHasTaint := r.hasTaintBySpec(node, rule.Spec.Taint)

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

func (r *RuleReconciler) ensureFinalizer(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule, finalizer string) (finalizerAdded bool, err error) {
	// Finalizers can only be added when the deletionTimestamp is not set.
	if !rule.GetDeletionTimestamp().IsZero() {
		return false, nil
	}
	if controllerutil.ContainsFinalizer(rule, finalizer) {
		return false, nil
	}

	added := false
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &readinessv1alpha1.NodeReadinessRule{}
		if err := r.Get(ctx, client.ObjectKey{Name: rule.Name}, latest); err != nil {
			return err
		}
		// The rule may have started deleting since the check above, and the API server
		// rejects new finalizers on a deleting object.
		if !latest.DeletionTimestamp.IsZero() || controllerutil.ContainsFinalizer(latest, finalizer) {
			return nil
		}

		stored := latest.DeepCopy()
		controllerutil.AddFinalizer(latest, finalizer)
		if err := r.Patch(ctx, latest, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}

		*rule = *latest
		added = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return added, nil
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

// SyncNodeStateMetrics synchronizes the NodesByState Prometheus metrics with the current rule status.
func (r *RuleReadinessController) SyncNodeStateMetrics(ctx context.Context, rule *readinessv1alpha1.NodeReadinessRule) {
	var ready, notReady, bootstrapping float64

	for _, eval := range rule.Status.NodeEvaluations {
		if eval.TaintStatus == readinessv1alpha1.TaintStatusAbsent {
			ready++
		} else {
			// The taint is still present.
			if rule.Spec.EnforcementMode == readinessv1alpha1.EnforcementModeBootstrapOnly {
				// In BootstrapOnly mode, if the taint is present, it is still bootstrapping.
				bootstrapping++
			} else {
				notReady++
			}
		}
	}

	metrics.NodesByState.WithLabelValues(rule.Name, string(metrics.NodeStateReady)).Set(ready)
	metrics.NodesByState.WithLabelValues(rule.Name, string(metrics.NodeStateNotReady)).Set(notReady)
	metrics.NodesByState.WithLabelValues(rule.Name, string(metrics.NodeStateBootstrapping)).Set(bootstrapping)
}
