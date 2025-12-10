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

// NodeReadinessRuleSpec defines the desired state of NodeReadinessRule
type NodeReadinessRuleSpec struct {
	// Replace single ConditionType with multiple conditions
	Conditions []ConditionRequirement `json:"conditions"`

	// Add enforcement mode
	EnforcementMode EnforcementMode `json:"enforcementMode"`

	// Simplify taint specification (remove TaintKey, TaintEffect separation)
	Taint TaintSpec `json:"taint"`

	// Keep existing fields
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`
	GracePeriod  *metav1.Duration      `json:"gracePeriod,omitempty"`

	// Add dry run support
	DryRun bool `json:"dryRun,omitempty"`
}

// New types to add
type ConditionRequirement struct {
	Type           string                 `json:"type"`
	RequiredStatus corev1.ConditionStatus `json:"requiredStatus"`
}

type TaintSpec struct {
	Key    string             `json:"key"`
	Effect corev1.TaintEffect `json:"effect"`
	Value  string             `json:"value,omitempty"`
}

type EnforcementMode string

const (
	EnforcementModeBootstrapOnly EnforcementMode = "bootstrap-only"
	EnforcementModeContinuous    EnforcementMode = "continuous"
)

// NodeReadinessRuleStatus defines the observed state of NodeReadinessRule.
type NodeReadinessRuleStatus struct {
	// Keep existing
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	AppliedNodes       []string           `json:"appliedNodes,omitempty"`

	// Add new status tracking
	NodeEvaluations []NodeEvaluation `json:"nodeEvaluations,omitempty"`
	FailedNodes     []NodeFailure    `json:"failedNodes,omitempty"`

	// Add dry run results
	DryRunResults *DryRunResults `json:"dryRunResults,omitempty"`
}

type NodeEvaluation struct {
	NodeName         string                      `json:"nodeName"`
	ConditionResults []ConditionEvaluationResult `json:"conditionResults"`
	TaintStatus      string                      `json:"taintStatus"` // "Present", "Absent", "Unknown"
	LastEvaluated    metav1.Time                 `json:"lastEvaluated"`
}

type ConditionEvaluationResult struct {
	Type           string                 `json:"type"`
	CurrentStatus  corev1.ConditionStatus `json:"currentStatus"`
	RequiredStatus corev1.ConditionStatus `json:"requiredStatus"`
	Satisfied      bool                   `json:"satisfied"`
	Missing        bool                   `json:"missing"`
}

type NodeFailure struct {
	NodeName    string      `json:"nodeName"`
	Reason      string      `json:"reason"`
	Message     string      `json:"message"`
	LastUpdated metav1.Time `json:"lastUpdated"`
}

type DryRunResults struct {
	AffectedNodes   int    `json:"affectedNodes"`
	TaintsToAdd     int    `json:"taintsToAdd"`
	TaintsToRemove  int    `json:"taintsToRemove"`
	RiskyOperations int    `json:"riskyOperations"`
	Summary         string `json:"summary"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=nrr
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.enforcementMode`,description="Continuous, Bootstrap or Dryrun - shows if the rule is in enforcement or dryrun mode."
// +kubebuilder:printcolumn:name="Taint",type=string,JSONPath=`.spec.taint.key`,description="The readiness taint applied by this rule."
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,description="The age of this resource"

// NodeReadinessRule is the Schema for the NodeReadinessRules API
type NodeReadinessRule struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of NodeReadinessRule
	// +required
	Spec NodeReadinessRuleSpec `json:"spec"`

	// status defines the observed state of NodeReadinessRule
	// +optional
	Status NodeReadinessRuleStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// NodeReadinessRuleList contains a list of NodeReadinessRule
type NodeReadinessRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeReadinessRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeReadinessRule{}, &NodeReadinessRuleList{})
}
