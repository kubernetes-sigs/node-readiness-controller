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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// NodeEvaluationState indicates the overall readiness state of the node based on all rules.
// +kubebuilder:validation:Enum=Ready;NotReady;Pending
type NodeEvaluationState string

const (
	// NodeEvaluationStateReady indicates the node has successfully satisfied all applicable rules.
	NodeEvaluationStateReady NodeEvaluationState = "Ready"

	// NodeEvaluationStateNotReady indicates one or more applicable rules are currently not satisfied.
	NodeEvaluationStateNotReady NodeEvaluationState = "NotReady"

	// NodeEvaluationStatePending indicates the node is currently being evaluated or state is unknown.
	NodeEvaluationStatePending NodeEvaluationState = "Pending"
)

// RuleStatus defines the result of evaluating a NodeReadinessRule's criteria against a Node.
// +kubebuilder:validation:Enum=Matched;Unmatched;Error
type RuleStatus string

const (
	// RuleStatusMatched indicates that the Node successfully met all criteria
	// (both NodeSelector and Conditions) defined in the NodeReadinessRule.
	// When in this state, the controller should ensure the corresponding Taint is applied.
	RuleStatusMatched RuleStatus = "Matched"

	// RuleStatusUnmatched indicates that the Node did not meet the criteria
	// defined in the NodeReadinessRule (e.g., label mismatch or condition not satisfied).
	// When in this state, the controller should ensure the corresponding Taint is absent.
	RuleStatusUnmatched RuleStatus = "Unmatched"

	// RuleStatusError indicates that a programmatic or configuration error occurred
	// during the evaluation process (e.g., an invalid or unparseable NodeSelector).
	// The controller cannot safely determine if the taint should be present or absent.
	RuleStatusError RuleStatus = "Error"
)

// NodeReadinessEvaluationSpec defines the desired state of NodeReadinessEvaluation.
type NodeReadinessEvaluationSpec struct {
	// nodeName specifies the exact name of the target Kubernetes Node.
	// This object establishes a strict 1:1 relationship with the specified node,
	// acting as the single source of truth for all rules and statuses applied to it.
	// Because it binds this resource to a specific physical or virtual machine, it cannot be changed once set.
	//
	// The validation constraints enforce standard Kubernetes resource naming
	// (RFC 1123 DNS Subdomain format), as defined in upstream apimachinery:
	// https://github.com/kubernetes/apimachinery/blob/master/pkg/util/validation/validation.go#L198
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="nodeName is immutable and cannot be changed once set"
	NodeName string `json:"nodeName,omitempty"`
}

// NodeReadinessEvaluationStatus defines the observed state of NodeReadinessEvaluation.
// +kubebuilder:validation:MinProperties=1
type NodeReadinessEvaluationStatus struct {
	// conditions represent the latest available observations of the node's readiness evaluation state.
	// Known condition types are:
	// - "Evaluated": indicates whether the controller successfully evaluated all rules without errors.
	// - "Ready": indicates whether the node has satisfied all rules and has zero taints applied.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// state indicates the overall readiness state of the node based on all applicable rules.
	// It acts as a top-level health indicator for this node's readiness evaluation.
	//
	// +optional
	State NodeEvaluationState `json:"state,omitempty"`

	// summary provides a quick, aggregated overview of the rules applied to this node.
	//
	// +optional
	Summary EvaluationSummary `json:"summary,omitempty,omitzero"`

	// rules contains the evaluation outcomes for all rules applicable to this node.
	// The slice is owned and fully replaced by the controller on each reconcile (listType=atomic).
	//
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=100
	Rules []RuleEvaluation `json:"rules,omitempty"`
}

// RuleRef identifies a NodeReadinessRule by its name and UID.
// Bundling both fields into a single struct makes the identity unit explicit and
// mirrors the pattern used by metav1.OwnerReference.
type RuleRef struct {
	// name is the name of the NodeReadinessRule.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// uid is the UID of the NodeReadinessRule.
	// If the rule is deleted and recreated with the same name, the UID will differ,
	// allowing the controller to detect and replace stale evaluation entries.
	//
	// +required
	UID types.UID `json:"uid,omitempty"`
}

// RuleEvaluation defines the outcome of evaluating a single NodeReadinessRule against this Node.
type RuleEvaluation struct {
	// ruleRef identifies the NodeReadinessRule this evaluation applies to.
	// It bundles the rule's name (human-readable) and UID (tamper-proof identity)
	// so that delete-and-recreate of a same-named rule is always detectable.
	//
	// +required
	RuleRef RuleRef `json:"ruleRef,omitempty,omitzero"`

	// ruleStatus indicates the overall outcome of the rule's criteria against the Node.
	//
	// +required
	RuleStatus RuleStatus `json:"ruleStatus,omitempty"`

	// taintStatus reflects the observed state of the rule's specified taint on the Node (Present/Absent).
	//
	// +required
	TaintStatus TaintStatus `json:"taintStatus,omitempty"`

	// reason contains a concise, machine-readable string detailing the primary outcome.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Reason string `json:"reason,omitempty"`

	// message is a comprehensive, human-readable explanation providing further context.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=10240
	Message string `json:"message,omitempty"`

	// readinessConditions provides a detailed breakdown of each condition evaluation
	// for this Node. This allows for granular debugging of which specific criteria passed/failed.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=32
	ReadinessConditions []ConditionEvaluationResult `json:"readinessConditions,omitempty"`

	// lastEvaluationTime records the exact moment the controller most recently assessed this rule.
	//
	// +required
	LastEvaluationTime metav1.Time `json:"lastEvaluationTime,omitempty,omitzero"`

	// firstEvaluatedAt is the time the rule was first assessed against this node.
	//
	// +optional
	FirstEvaluatedAt *metav1.Time `json:"firstEvaluatedAt,omitempty"`

	// taintObservedAt is the time NRC first observed the taint present on the node,
	// regardless of whether NRC applied it or it was pre-existing (e.g. via --register-with-taints).
	// This marks the beginning of the node being blocked by this rule and is the correct
	// start time for computing time-to-unblock SLIs.
	//
	// +optional
	TaintObservedAt *metav1.Time `json:"taintObservedAt,omitempty"`

	// taintAddedAt is the time NRC itself applied the taint to the node.
	// This field is nil when the taint was pre-existing and NRC adopted it rather than creating it.
	// Use taintObservedAt for "how long has the node been blocked"; use this field to measure
	// NRC's own apply latency (taintAddedAt - firstEvaluatedAt).
	//
	// +optional
	TaintAddedAt *metav1.Time `json:"taintAddedAt,omitempty"`

	// taintRemovedAt is the time the controller successfully removed the taint
	// after the node satisfied all conditions.
	//
	// +optional
	TaintRemovedAt *metav1.Time `json:"taintRemovedAt,omitempty"`
}

// EvaluationSummary aggregates the results to provide a high-level overview.
// +kubebuilder:validation:MinProperties=1
type EvaluationSummary struct {
	// matchedRules is the total number of rules currently matching this node.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	MatchedRules *int32 `json:"matchedRules,omitempty"`

	// unmatchedRules is the total number of rules currently not matching this node.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	UnmatchedRules *int32 `json:"unmatchedRules,omitempty"`

	// activeTaints is the total number of taints successfully managed by the controller.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	ActiveTaints *int32 `json:"activeTaints,omitempty"`

	// errors is the total number of rules that failed to evaluate properly.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	Errors *int32 `json:"errors,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=nre
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.nodeName`,description="The name of the target Node."
// +kubebuilder:selectablefield:JSONPath=`.spec.nodeName`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`,description="The overall readiness evaluation state of the node."
// +kubebuilder:selectablefield:JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,description="The age of this resource."

// NodeReadinessEvaluation is the Schema for the NodeReadinessEvaluations API.
// Each instance maps 1:1 to a Node and folds the outcomes of all applicable
// NodeReadinessRules for that node into a single object.
// An ownerReference to the corresponding Node is set for automatic garbage
// collection when the node is deleted.
type NodeReadinessEvaluation struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	//
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of NodeReadinessEvaluation.
	//
	// +required
	Spec NodeReadinessEvaluationSpec `json:"spec,omitempty,omitzero"`

	// status defines the observed state of NodeReadinessEvaluation.
	//
	// +optional
	Status NodeReadinessEvaluationStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// NodeReadinessEvaluationList contains a list of NodeReadinessEvaluation.
type NodeReadinessEvaluationList struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard list's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#lists-and-simple-kinds
	//
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// items is the list of NodeReadinessEvaluation.
	Items []NodeReadinessEvaluation `json:"items"`
}

func init() {
	objectTypes = append(objectTypes, &NodeReadinessEvaluation{}, &NodeReadinessEvaluationList{})
}
