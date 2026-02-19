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
	// +required
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

	// lastEvaluationTime is the timestamp when the rule was evaluated against all the nodes in the cluster.
	//
	// +required
	LastEvaluationTime metav1.Time `json:"lastEvaluationTime,omitempty,omitzero"`

	// dryRunResults captures the outcome of the rule evaluation when DryRun is enabled.
	// This field provides visibility into the actions the controller would have taken,
	// allowing users to preview taint changes before they are committed.
	//
	// +optional
	DryRunResults DryRunResults `json:"dryRunResults,omitempty,omitzero"`
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
