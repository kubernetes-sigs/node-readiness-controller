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
)

// EnforcementMode specifies how the controller maintains the desired state.
// +kubebuilder:validation:Enum=bootstrap-only;continuous
type EnforcementMode string

const (
	// EnforcementModeBootstrapOnly applies configuration only during the first reconcile.
	EnforcementModeBootstrapOnly EnforcementMode = "bootstrap-only"

	// EnforcementModeContinuous continuously monitors and enforces the configuration.
	EnforcementModeContinuous EnforcementMode = "continuous"
)

// TaintStatus specifies status of the Taint on Node.
// +kubebuilder:validation:Enum=Present;Absent
type TaintStatus string

const (
	// TaintStatusPresent represent the taint present on the Node.
	TaintStatusPresent TaintStatus = "Present"

	// TaintStatusAbsent represent the taint absent on the Node.
	TaintStatusAbsent TaintStatus = "Absent"
)

// NodeReadinessRuleSpec defines the desired state of NodeReadinessRule.
type NodeReadinessRuleSpec struct {
	// conditions contains a list of the Node conditions that defines the specific
	// criteria that must be met for taints to be managed on the target Node.
	// The presence or status of these conditions directly triggers the application or removal of Node taints.
	//
	// +required
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	Conditions []ConditionRequirement `json:"conditions"` //nolint:kubeapilinter

	// enforcementMode specifies how the controller maintains the desired state.
	// enforcementMode is one of bootstrap-only, continuous.
	// "bootstrap-only" applies the configuration once during initial setup.
	// "continuous" ensures the state is monitored and corrected throughout the resource lifecycle.
	//
	// +optional
	EnforcementMode EnforcementMode `json:"enforcementMode,omitempty"`

	// taint defines the specific Taint (Key, Value, and Effect) to be managed
	// on Nodes that meet the defined condition criteria.
	//
	// +required
	Taint corev1.Taint `json:"taint,omitempty,omitzero"`

	// nodeSelector limits the scope of this rule to a specific subset of Nodes.
	//
	// +required
	NodeSelector metav1.LabelSelector `json:"nodeSelector,omitempty,omitzero"`

	// dryRun when set to true, The controller will evaluate Node conditions and log intended taint modifications
	// without persisting changes to the cluster. Proposed actions are reflected in the resource status.
	//
	// +optional
	DryRun bool `json:"dryRun,omitempty"` //nolint:kubeapilinter
}

// ConditionRequirement defines a specific Node condition and the status value
// required to trigger the controller's action.
type ConditionRequirement struct {
	// type of Node condition
	//
	// Following kubebuilder validation is referred from https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Condition
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=316
	Type string `json:"type,omitempty"`

	// requiredStatus is status of the condition, one of True, False, Unknown.
	//
	// +required
	// +kubebuilder:validation:Enum=True;False;Unknown
	RequiredStatus corev1.ConditionStatus `json:"requiredStatus,omitempty"`
}

// NodeReadinessRuleStatus defines the observed state of NodeReadinessRule.
// +kubebuilder:validation:MinProperties=1
type NodeReadinessRuleStatus struct {
	// observedGeneration reflects the generation of the most recently observed NodeReadinessRule by the controller.
	//
	// +optional
	// +kubebuilder:validation:Minimum=1
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// appliedNodes lists the names of Nodes where the taint has been successfully managed.
	// This provides a quick reference to the scope of impact for this rule.
	//
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=5000
	// +kubebuilder:validation:items:MaxLength=253
	AppliedNodes []string `json:"appliedNodes,omitempty"`

	// failedNodes lists the Nodes where the rule evaluation encountered an error.
	// This is used for troubleshooting configuration issues, such as invalid selectors during node lookup.
	//
	// +optional
	// +listType=map
	// +listMapKey=nodeName
	// +kubebuilder:validation:MaxItems=5000
	FailedNodes []NodeFailure `json:"failedNodes,omitempty"`

	// nodeEvaluations provides detailed insight into the rule's assessment for individual Nodes.
	// This is primarily used for auditing and debugging why specific Nodes were or
	// were not targeted by the rule.
	//
	// +optional
	// +listType=map
	// +listMapKey=nodeName
	// +kubebuilder:validation:MaxItems=5000
	NodeEvaluations []NodeEvaluation `json:"nodeEvaluations,omitempty"`

	// dryRunResults captures the outcome of the rule evaluation when DryRun is enabled.
	// This field provides visibility into the actions the controller would have taken,
	// allowing users to preview taint changes before they are committed.
	//
	// +optional
	DryRunResults DryRunResults `json:"dryRunResults,omitempty,omitzero"`
}

// NodeFailure provides diagnostic details for Nodes that could not be successfully evaluated by the rule.
type NodeFailure struct {
	// nodeName is the name of the failed Node.
	//
	// Following kubebuilder validation is referred from
	// https://github.com/kubernetes/apimachinery/blob/84d740c9e27f3ccc94c8bc4d13f1b17f60f7080b/pkg/util/validation/validation.go#L198
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	NodeName string `json:"nodeName,omitempty"`

	// reason provides a brief explanation of the evaluation result.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Reason string `json:"reason,omitempty"`

	// message is a human-readable message indicating details about the evaluation.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=10240
	Message string `json:"message,omitempty"`

	// lastEvaluationTime is the timestamp of the last rule check failed for this Node.
	//
	// +required
	LastEvaluationTime metav1.Time `json:"lastEvaluationTime,omitempty,omitzero"`
}

// NodeEvaluation provides a detailed audit of a single Node's compliance with the rule.
type NodeEvaluation struct {
	// nodeName is the name of the evaluated Node.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	NodeName string `json:"nodeName,omitempty"`

	// conditionResults provides a detailed breakdown of each condition evaluation
	// for this Node. This allows for granular auditing of which specific
	// criteria passed or failed during the rule assessment.
	//
	// +required
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=5000
	ConditionResults []ConditionEvaluationResult `json:"conditionResults,omitempty"`

	// taintStatus represents the taint status on the Node, one of Present, Absent.
	//
	// +required
	TaintStatus TaintStatus `json:"taintStatus,omitempty"`

	// lastEvaluationTime is the timestamp when the controller last assessed this Node.
	//
	// +required
	LastEvaluationTime metav1.Time `json:"lastEvaluationTime,omitempty,omitzero"`
}

// ConditionEvaluationResult provides a detailed report of the comparison between
// the Node's observed condition and the rule's requirement.
type ConditionEvaluationResult struct {
	// type corresponds to the Node condition type being evaluated.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=316
	Type string `json:"type,omitempty"`

	// currentStatus is the actual status value observed on the Node, one of True, False, Unknown.
	//
	// +required
	// +kubebuilder:validation:Enum=True;False;Unknown
	CurrentStatus corev1.ConditionStatus `json:"currentStatus,omitempty"`

	// requiredStatus is the status value defined in the rule that must be matched, one of True, False, Unknown.
	//
	// +required
	// +kubebuilder:validation:Enum=True;False;Unknown
	RequiredStatus corev1.ConditionStatus `json:"requiredStatus,omitempty"`

	// satisfied indicates whether the CurrentStatus matches the RequiredStatus.
	//
	// +required
	Satisfied bool `json:"satisfied"` //nolint:kubeapilinter

	// missing indicates that the specified condition was not found on the Node.
	//
	// +required
	Missing bool `json:"missing"` //nolint:kubeapilinter
}

// DryRunResults provides a summary of the actions the controller would perform if DryRun mode is enabled.
// +kubebuilder:validation:MinProperties=1
type DryRunResults struct {
	// affectedNodes is the total count of Nodes that match the rule's criteria.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	AffectedNodes *int32 `json:"affectedNodes,omitempty"`

	// taintsToAdd is the number of Nodes that currently lack the specified taint and would have it applied.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	TaintsToAdd *int32 `json:"taintsToAdd,omitempty"`

	// taintsToRemove is the number of Nodes that currently possess the
	// taint but no longer meet the criteria, leading to its removal.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	TaintsToRemove *int32 `json:"taintsToRemove,omitempty"`

	// riskyOperations represents the count of Nodes where required conditions
	// are missing entirely, potentially indicating an ambiguous node state.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	RiskyOperations *int32 `json:"riskyOperations,omitempty"`

	// summary provides a human-readable overview of the dry run evaluation,
	// highlighting key findings or warnings.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	Summary string `json:"summary,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=nrr
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.enforcementMode`,description="Continuous, Bootstrap or Dryrun - shows if the rule is in enforcement or dryrun mode."
// +kubebuilder:printcolumn:name="Taint",type=string,JSONPath=`.spec.taint.key`,description="The readiness taint applied by this rule."
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,description="The age of this resource"

// NodeReadinessRule is the Schema for the NodeReadinessRules API.
type NodeReadinessRule struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	//
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of NodeReadinessRule
	//
	// +required
	Spec NodeReadinessRuleSpec `json:"spec,omitempty,omitzero"`

	// status defines the observed state of NodeReadinessRule
	//
	// +optional
	Status NodeReadinessRuleStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// NodeReadinessRuleList contains a list of NodeReadinessRule.
type NodeReadinessRuleList struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard list's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#lists-and-simple-kinds
	//
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// items is the list of NodeReadinessRule.
	Items []NodeReadinessRule `json:"items"`
}

func init() {
	objectTypes = append(objectTypes, &NodeReadinessRule{}, &NodeReadinessRuleList{})
}
