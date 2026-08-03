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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// NodeEvaluationState indicates the overall readiness/availability state of the node based on all rules.
// +kubebuilder:validation:Enum=Available;NotAvailable
type NodeEvaluationState string

const (
	// NodeEvaluationStateAvailable indicates the node has satisfied all applicable rules and is available for scheduling.
	NodeEvaluationStateAvailable NodeEvaluationState = "Available"

	// NodeEvaluationStateNotAvailable indicates one or more applicable rules are currently not satisfied.
	NodeEvaluationStateNotAvailable NodeEvaluationState = "NotAvailable"
)

// RuleStatus defines the result of evaluating a NodeReadinessRule's criteria against a Node.
// Rule-configuration faults (e.g. an invalid NodeSelector) are reported on the
// NodeReadinessRule's own status conditions, not here.
// +kubebuilder:validation:Enum=Satisfied;Unsatisfied
type RuleStatus string

const (
	// RuleStatusSatisfied indicates that the Node successfully met all conditions
	// defined in the NodeReadinessRule. The controller will ensure the corresponding
	// taint is removed so the node is unblocked.
	RuleStatusSatisfied RuleStatus = "Satisfied"

	// RuleStatusUnsatisfied indicates that one or more conditions defined in the
	// NodeReadinessRule were not met. The controller will ensure the corresponding
	// taint remains present to block scheduling.
	RuleStatusUnsatisfied RuleStatus = "Unsatisfied"
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
	// - "Available": indicates whether the node has satisfied all rules, has zero taints applied, and is available for scheduling.
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

	// rules contains the evaluation outcomes for all rules applicable to this node.
	// Each entry is keyed by ruleName, allowing independent per-rule updates by
	// parallel rule-workers without last-write-wins conflicts (listType=map).
	//
	// +optional
	// +listType=map
	// +listMapKey=ruleName
	// +kubebuilder:validation:MaxItems=100
	Rules []RuleEvaluation `json:"rules,omitempty"`
}

// RuleEvaluation defines the outcome of evaluating a single NodeReadinessRule against this Node.
type RuleEvaluation struct {
	// ruleName is the name of the NodeReadinessRule.
	// This field is the map key for status.rules (listType=map) and must always be present.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	RuleName string `json:"ruleName,omitempty"`

	// ruleUID is the UID of the NodeReadinessRule.
	// If the rule is deleted and recreated with the same name, the UID will differ,
	// allowing the controller to detect and replace stale evaluation entries.
	//
	// +required
	RuleUID types.UID `json:"ruleUID,omitempty"`

	// ruleStatus indicates the overall outcome of the rule's criteria against the Node.
	//
	// +required
	RuleStatus RuleStatus `json:"ruleStatus,omitempty"`

	// taintStatus reflects the observed state of the rule's specified taint on the Node (Present/Absent).
	//
	// +required
	TaintStatus TaintStatus `json:"taintStatus,omitempty"`

	// taintKey is the key of the taint managed by this rule, stamped at evaluation
	// time so this entry is self-contained without requiring a lookup of the rule.
	// Matches rule.spec.taint.key.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	TaintKey string `json:"taintKey,omitempty"`

	// taintEffect is the effect of the taint managed by this rule, stamped at
	// evaluation time so this entry is self-contained without requiring a lookup
	// of the rule. Matches rule.spec.taint.effect.
	//
	// +required
	// +kubebuilder:validation:Enum=NoSchedule;PreferNoSchedule;NoExecute
	TaintEffect corev1.TaintEffect `json:"taintEffect,omitempty"`

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

	// taintAddedAt is the time NRC itself first applied the taint to the node in the
	// current taint lifecycle (i.e., since the last removal). This field is nil when
	// the taint was pre-existing and NRC adopted it rather than creating it.
	// The value is set once on the Absent→Present transition and carried forward on
	// subsequent reconciles; it is not the timestamp of the most recent reconcile.
	// Use taintObservedAt for "how long has the node been blocked"; use this field
	// to measure NRC's own apply latency (taintAddedAt - firstEvaluatedAt).
	//
	// +optional
	TaintAddedAt *metav1.Time `json:"taintAddedAt,omitempty"`

	// taintRemovedAt is the time the controller successfully removed the taint
	// after the node satisfied all conditions.
	//
	// +optional
	TaintRemovedAt *metav1.Time `json:"taintRemovedAt,omitempty"`
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
