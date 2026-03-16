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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=nrrp
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.nodeName`,description="The Node this report belongs to."
// +kubebuilder:printcolumn:name="Matched Rules",type=integer,JSONPath=`.status.summary.matchedRules`,description="Number of rules matching this node."
// +kubebuilder:printcolumn:name="UnMatched Rules",type=integer,JSONPath=`.status.summary.unMatchedRules`,description="Number of rules not matching this node."
// +kubebuilder:printcolumn:name="Applied Taints",type=integer,JSONPath=`.status.summary.appliedTaints`,description="Number of taints currently applied."
// +kubebuilder:printcolumn:name="Errors",type=integer,JSONPath=`.status.summary.errors`,description="Number of evaluation errors."
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NodeReadinessRuleReport is the Schema for the nodereadinessrulereports API.
type NodeReadinessRuleReport struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of NodeReadinessRuleReport
	// +required
	Spec NodeReadinessRuleReportSpec `json:"spec,omitzero"`

	// status defines the observed state of NodeReadinessRuleReport
	// +optional
	Status NodeReadinessRuleReportStatus `json:"status,omitzero"`
}

// NodeReadinessRuleReportSpec defines the desired state of NodeReadinessRuleReport.
type NodeReadinessRuleReportSpec struct {
	// nodeName specifies the exact name of the target Kubernetes Node.
	// This object establishes a strict 1:1 relationship with the specified node,
	// acting as the single source of truth for all rules and statuses applied to it.
	// Because it binds this resource to a specific  physical or virtual machine, it cannot be changed once set.
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

// NodeReadinessRuleReportStatus defines the observed state of NodeReadinessRuleReport.
// +kubebuilder:validation:MinProperties=1
type NodeReadinessRuleReportStatus struct {
	// readinessReports provides detailed insight into the rule's assessment for individual Nodes.
	// This is primarily used for auditing and debugging why specific Nodes were or
	// were not targeted by the rule.
	//
	// +optional
	// +listType=map
	// +listMapKey=ruleName
	// +kubebuilder:validation:MaxItems=100
	ReadinessReports []ReadinessReport `json:"readinessReports,omitempty"`

	// summary provides a quick overview of the rules applied to this node.
	//
	// +optional
	Summary ReportSummary `json:"summary,omitempty,omitzero"`
}

// ReadinessReport defines the outcome of evaluating a single NodeReadinessRule against a specific Node.
type ReadinessReport struct {
	// ruleName is the name of the NodeReadinessRule being evaluated.
	// It acts as a direct reference to the NodeReadinessRule that generated this report entry.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	RuleName string `json:"ruleName,omitempty"`

	// reason contains a concise, machine-readable string detailing the primary outcome
	// of the evaluation (e.g., "SelectorMismatch", "CriteriaMet", "ConditionNotFound").
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Reason string `json:"reason,omitempty"`

	// message is a comprehensive, human-readable explanation providing further
	// context about the evaluation result or any specific errors encountered.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=10240
	Message string `json:"message,omitempty"`

	// ruleStatus indicates the overall outcome of the rule's criteria against the Node.
	// This reflects whether the Node successfully matched the rule's NodeSelector and Conditions.
	//
	// +required
	RuleStatus RuleStatus `json:"ruleStatus,omitempty"`

	// taintStatus reflects the observed state of the rule's specified taint on the Node.
	// It indicates whether the taint is currently Present or Absent.
	//
	// +required
	TaintStatus TaintStatus `json:"taintStatus,omitempty"`

	// lastEvaluationTime records the exact moment the controller most recently
	// assessed this rule against the Node. This helps identify stale reports.
	//
	// +required
	LastEvaluationTime metav1.Time `json:"lastEvaluationTime,omitempty,omitzero"`
}

// ReportSummary aggregates the results to provide a high-level overview.
// +kubebuilder:validation:MinProperties=1
type ReportSummary struct {
	// matchedRules is the total number of rules currently matching this node.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	MatchedRules *int32 `json:"matchedRules,omitempty"`

	// unMatchedRules is the total number of rules currently not matching this node.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	UnMatchedRules *int32 `json:"unMatchedRules,omitempty"`

	// appliedTaints is the total number of taints successfully applied by the controller.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	AppliedTaints *int32 `json:"appliedTaints,omitempty"`

	// errors is the total number of rules that failed to evaluate properly.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	Errors *int32 `json:"errors,omitempty"`
}

// +kubebuilder:object:root=true

// NodeReadinessRuleReportList contains a list of NodeReadinessRuleReport.
type NodeReadinessRuleReportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []NodeReadinessRuleReport `json:"items"`
}

func init() {
	objectTypes = append(objectTypes, &NodeReadinessRuleReport{}, &NodeReadinessRuleReportList{})
}
