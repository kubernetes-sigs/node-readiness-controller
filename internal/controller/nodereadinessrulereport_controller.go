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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

// NodeReadinessRuleReportReconciler reconciles a NodeReadinessRuleReport object.
type NodeReadinessRuleReportReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	Controller *RuleReadinessController
}

func (r *NodeReadinessRuleReportReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("nodereadinessrulereport").
		For(&corev1.Node{}).
		Owns(&readinessv1alpha1.NodeReadinessRuleReport{}).
		Watches(
			&readinessv1alpha1.NodeReadinessRule{},
			handler.EnqueueRequestsFromMapFunc(r.mapRuleToNodes),
		).
		Complete(r)
}

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrules,verbs=get;list;watch
// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrulereports,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrulereports/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=readiness.node.x-k8s.io,resources=nodereadinessrulereports/finalizers,verbs=update

func (r *NodeReadinessRuleReportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// 1. Fetch the Target Node
	var node corev1.Node
	if err := r.Get(ctx, req.NamespacedName, &node); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log = log.WithValues("node", node.Name)
	ctx = ctrl.LoggerInto(ctx, log)
	log.Info("Generating NodeReadinessRules report")

	// 2. Fetch all NodeReadinessRules
	var ruleList readinessv1alpha1.NodeReadinessRuleList
	if err := r.List(ctx, &ruleList); err != nil {
		log.Error(err, "Failed to list NodeReadinessRules")
		return ctrl.Result{}, err
	}

	// 3. Evaluate all rules against this Node
	readinessReports := make([]readinessv1alpha1.ReadinessReport, 0, len(ruleList.Items))
	for _, rule := range ruleList.Items {
		log.Info("Evaluating rule", "ruleName", rule.Name)
		report := r.evaluateNode(ctx, node, rule)
		log.Info("Rule report", "ruleName", rule.Name, "report", report)
		readinessReports = append(readinessReports, report)
	}

	sort.SliceStable(readinessReports, func(i, j int) bool {
		return readinessReports[i].RuleName < readinessReports[j].RuleName
	})

	// 4. Calculate the Summary metrics
	var matchedRules, unMatchedRules, appliedTaints, evalErrors int32

	for _, result := range readinessReports {
		if result.RuleStatus == readinessv1alpha1.RuleStatusMatched {
			matchedRules++
		}

		if result.RuleStatus == readinessv1alpha1.RuleStatusUnmatched {
			unMatchedRules++
		}

		if result.TaintStatus == readinessv1alpha1.TaintStatusPresent {
			appliedTaints++
		}

		if result.RuleStatus == readinessv1alpha1.RuleStatusError {
			evalErrors++
		}
	}

	// Create the summary object
	reportSummary := readinessv1alpha1.ReportSummary{
		MatchedRules:   &matchedRules,
		UnMatchedRules: &unMatchedRules,
		AppliedTaints:  &appliedTaints,
		Errors:         &evalErrors,
	}
	log.V(5).Info("Rules summary report", "summary", reportSummary)
	// 5. Initialize the NodeReadinessRuleReport Object.
	report := &readinessv1alpha1.NodeReadinessRuleReport{
		ObjectMeta: metav1.ObjectMeta{
			Name: getNodeReadinessRuleReportName(node.Name),
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, report, func() error {
		if err := controllerutil.SetControllerReference(&node, report, r.Scheme); err != nil {
			return err
		}
		report.Spec.NodeName = node.Name
		return nil
	})

	if err != nil {
		log.Error(err, "Failed to create or update NodeReadinessReport Spec")
		return ctrl.Result{}, err
	}

	oldStatus := report.Status.DeepCopy()

	report.Status.ReadinessReports = readinessReports
	report.Status.Summary = reportSummary

	if !equality.Semantic.DeepEqual(oldStatus, &report.Status) {
		if err := r.Status().Update(ctx, report); err != nil {
			log.Error(err, "Failed to update NodeReadinessReport Status")
			return ctrl.Result{}, err
		}
		log.Info("Successfully updated NodeReadinessReport Status")
	}

	if op != controllerutil.OperationResultNone {
		log.Info("Successfully reconciled NodeReadinessReport Spec", "operation", op)
	}

	return ctrl.Result{}, nil
}

// evaluateNode checks a single Node against a single NodeReadinessRule
// and returns a populated NodeReadinessRuleReport for the NodeReadinessRule.
func (r *NodeReadinessRuleReportReconciler) evaluateNode(ctx context.Context, node corev1.Node, rule readinessv1alpha1.NodeReadinessRule) readinessv1alpha1.ReadinessReport {
	log := ctrl.LoggerFrom(ctx)

	result := readinessv1alpha1.ReadinessReport{
		RuleName:           rule.Name,
		RuleStatus:         readinessv1alpha1.RuleStatusUnmatched,
		TaintStatus:        readinessv1alpha1.TaintStatusAbsent,
		LastEvaluationTime: metav1.Now(),
	}

	// 1. Evaluate the NodeSelector
	selector, err := metav1.LabelSelectorAsSelector(&rule.Spec.NodeSelector)
	if err != nil {
		log.Error(err, "Failed to convert node selector")
		result.Reason = "InvalidSelector"
		result.Message = fmt.Sprintf("Failed to parse NodeSelector: %v", err)
		result.RuleStatus = readinessv1alpha1.RuleStatusError
		return result
	}

	if !selector.Matches(labels.Set(node.Labels)) {
		result.Reason = "SelectorMismatch"
		result.Message = "Node labels do not match the rule's NodeSelector."
		return result
	}

	// 2. Check if the Taint is currently applied on the Node
	if hasTaint(&node, &rule.Spec.Taint) {
		result.TaintStatus = readinessv1alpha1.TaintStatusPresent
	}

	// 3. Evaluate the Conditions
	for _, req := range rule.Spec.Conditions {
		conditionFound := false

		for _, nodeCond := range node.Status.Conditions {
			if string(nodeCond.Type) == req.Type {
				conditionFound = true
				if nodeCond.Status != req.RequiredStatus {
					result.Reason = "ConditionStatusMismatch"
					result.Message = fmt.Sprintf("Condition '%s' is '%s', required '%s'.", req.Type, nodeCond.Status, req.RequiredStatus)
					return result
				}
				break // Found the condition and it matched, move to the next requirement
			}
		}

		if !conditionFound {
			log.Info("Condition not found", "type", req.Type)
			result.Reason = "ConditionNotFound"
			result.Message = fmt.Sprintf("Required condition '%s' was not found on the Node.", req.Type)
			return result
		}
	}

	// If we reach this point, the Node matched BOTH the selector and all conditions.
	result.Reason = "CriteriaMet"
	result.Message = "Node successfully matches all rule criteria."
	result.RuleStatus = readinessv1alpha1.RuleStatusMatched

	return result
}

// mapRuleToNodes is triggered whenever a NodeReadinessRule is Created, Updated, or Deleted.
// It queues a Reconcile request for every Node in the cluster.
func (r *NodeReadinessRuleReportReconciler) mapRuleToNodes(ctx context.Context, obj client.Object) []reconcile.Request {
	log := ctrl.LoggerFrom(ctx)

	var nodeList corev1.NodeList
	if err := r.List(ctx, &nodeList); err != nil {
		log.Error(err, "Failed to list nodes in mapRuleToNodes")
		return nil
	}

	requests := make([]reconcile.Request, 0, len(nodeList.Items))
	for _, node := range nodeList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: node.Name,
			},
		})
	}

	return requests
}

// hasTaint is a small helper to check if a specific taint exists on a Node.
func hasTaint(node *corev1.Node, targetTaint *corev1.Taint) bool {
	for _, t := range node.Spec.Taints {
		if t.Key == targetTaint.Key && t.Value == targetTaint.Value && t.Effect == targetTaint.Effect {
			return true
		}
	}
	return false
}

func getNodeReadinessRuleReportName(nodeName string) string {
	return fmt.Sprintf("nrr-report-%s", nodeName)
}
