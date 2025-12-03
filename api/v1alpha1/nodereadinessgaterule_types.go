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

// NodeReadinessGateRuleSpec defines the desired state of NodeReadinessGateRule
type NodeReadinessGateRuleSpec struct {
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

// NodeReadinessGateRuleStatus defines the observed state of NodeReadinessGateRule.
type NodeReadinessGateRuleStatus struct {
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
// +kubebuilder:resource:scope=Cluster,shortName=nrgr

// NodeReadinessGateRule is the Schema for the nodereadinessgaterules API
type NodeReadinessGateRule struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of NodeReadinessGateRule
	// +required
	Spec NodeReadinessGateRuleSpec `json:"spec"`

	// status defines the observed state of NodeReadinessGateRule
	// +optional
	Status NodeReadinessGateRuleStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// NodeReadinessGateRuleList contains a list of NodeReadinessGateRule
type NodeReadinessGateRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeReadinessGateRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeReadinessGateRule{}, &NodeReadinessGateRuleList{})
}
