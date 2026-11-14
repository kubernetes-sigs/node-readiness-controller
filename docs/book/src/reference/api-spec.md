# API Reference

## Packages
- [readiness.node.x-k8s.io/v1alpha1](#readinessnodex-k8siov1alpha1)


## readiness.node.x-k8s.io/v1alpha1

Package v1alpha1 contains API Schema definitions for the  v1alpha1 API group.

### Resource Types
- [NodeReadinessEvaluation](#nodereadinessevaluation)
- [NodeReadinessRule](#nodereadinessrule)



#### ConditionEvaluationResult



ConditionEvaluationResult provides a detailed report of the comparison between
the Node's observed condition and the rule's requirement.



_Appears in:_
- [NodeEvaluation](#nodeevaluation)
- [RuleEvaluation](#ruleevaluation)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | type corresponds to the Node condition type being evaluated. |  | MaxLength: 316 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `currentStatus` _[ConditionStatus](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#conditionstatus-v1-core)_ | currentStatus is the actual status value observed on the Node, one of True, False, Unknown. |  | Enum: [True False Unknown] <br />Required: \{\} <br /> |
| `requiredStatus` _[ConditionStatus](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#conditionstatus-v1-core)_ | requiredStatus is the status value defined in the rule that must be matched, one of True, False, Unknown. |  | Enum: [True False Unknown] <br />Required: \{\} <br /> |
| `defaultStatus` _[ConditionStatus](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#conditionstatus-v1-core)_ | defaultStatus is the status a condition is evaluated to if the condition<br />is not found in a node. Reflects the defaultStatus configured in the rule<br />spec. |  | Enum: [True False Unknown] <br />Optional: \{\} <br /> |


#### ConditionPolicy

_Underlying type:_ _string_

ConditionPolicy defines how the list of conditions is aggregated when evaluating a rule.

_Validation:_
- Enum: [allOf anyOf]

_Appears in:_
- [NodeReadinessRuleSpec](#nodereadinessrulespec)
- [RuleEvaluation](#ruleevaluation)

| Field | Description |
| --- | --- |
| `allOf` | ConditionPolicyAllOf requires ALL conditions to match their requiredStatus (default).<br /> |
| `anyOf` | ConditionPolicyAnyOf requires at least ONE condition to match its requiredStatus.<br /> |


#### ConditionRequirement



ConditionRequirement defines a specific Node condition and the status value
required to trigger the controller's action. It also contains an optional
default status value.



_Appears in:_
- [NodeReadinessRuleSpec](#nodereadinessrulespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | type of Node condition<br />Following kubebuilder validation is referred from https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Condition |  | MaxLength: 316 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `requiredStatus` _[ConditionStatus](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#conditionstatus-v1-core)_ | requiredStatus is status of the condition, one of True, False, Unknown. |  | Enum: [True False Unknown] <br />Required: \{\} <br /> |
| `defaultStatus` _[ConditionStatus](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#conditionstatus-v1-core)_ | defaultStatus is the status a condition is evaluated to if the condition<br />is not found in a node.<br />Accepted values are True, False, Unknown. It is optional.<br />When omitted, the effective default is Unknown, applied transparently by<br />the controller at evaluation time.<br />Note: This field must not be set when enforcementMode is bootstrap-only. |  | Enum: [True False Unknown] <br />Optional: \{\} <br /> |


#### DryRunResults



DryRunResults provides a summary of the actions the controller would perform if DryRun mode is enabled.

_Validation:_
- MinProperties: 1

_Appears in:_
- [NodeReadinessRuleStatus](#nodereadinessrulestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `affectedNodes` _integer_ | affectedNodes is the total count of Nodes that match the rule's criteria. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `taintsToAdd` _integer_ | taintsToAdd is the number of Nodes that currently lack the specified taint and would have it applied. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `taintsToRemove` _integer_ | taintsToRemove is the number of Nodes that currently possess the<br />taint but no longer meet the criteria, leading to its removal. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `riskyOperations` _integer_ | riskyOperations represents the count of Nodes where required conditions<br />are missing entirely, potentially indicating an ambiguous node state. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `summary` _string_ | summary provides a human-readable overview of the dry run evaluation,<br />highlighting key findings or warnings. |  | MaxLength: 4096 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### EnforcementMode

_Underlying type:_ _string_

EnforcementMode specifies how the controller maintains the desired state.

_Validation:_
- Enum: [bootstrap-only continuous]

_Appears in:_
- [NodeReadinessRuleSpec](#nodereadinessrulespec)

| Field | Description |
| --- | --- |
| `bootstrap-only` | EnforcementModeBootstrapOnly applies configuration only during the first reconcile.<br /> |
| `continuous` | EnforcementModeContinuous continuously monitors and enforces the configuration.<br /> |


#### NodeEvaluation



NodeEvaluation provides a detailed audit of a single Node's compliance with the rule.



_Appears in:_
- [NodeReadinessRuleStatus](#nodereadinessrulestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeName` _string_ | nodeName is the name of the evaluated Node. |  | MaxLength: 253 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` <br />Required: \{\} <br /> |
| `conditionResults` _[ConditionEvaluationResult](#conditionevaluationresult) array_ | conditionResults provides a detailed breakdown of each condition evaluation<br />for this Node. This allows for granular auditing of which specific<br />criteria passed or failed during the rule assessment. |  | MaxItems: 5000 <br />Required: \{\} <br /> |
| `taintStatus` _[TaintStatus](#taintstatus)_ | taintStatus represents the taint status on the Node, one of Present, Absent. |  | Enum: [Present Absent] <br />Required: \{\} <br /> |
| `lastEvaluationTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#time-v1-meta)_ | lastEvaluationTime is the timestamp when the controller last assessed this Node. |  | Required: \{\} <br /> |


#### NodeEvaluationState

_Underlying type:_ _string_

NodeEvaluationState indicates the overall readiness/availability state of the node based on all rules.

_Validation:_
- Enum: [Available NotAvailable]

_Appears in:_
- [NodeReadinessEvaluationStatus](#nodereadinessevaluationstatus)

| Field | Description |
| --- | --- |
| `Available` | NodeEvaluationStateAvailable indicates the node has satisfied all applicable rules and is available for scheduling.<br /> |
| `NotAvailable` | NodeEvaluationStateNotAvailable indicates one or more applicable rules are currently not satisfied.<br /> |


#### NodeFailure



NodeFailure provides diagnostic details for Nodes that could not be successfully evaluated by the rule.



_Appears in:_
- [NodeReadinessRuleStatus](#nodereadinessrulestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeName` _string_ | nodeName is the name of the failed Node.<br />Following kubebuilder validation is referred from<br />https://github.com/kubernetes/apimachinery/blob/84d740c9e27f3ccc94c8bc4d13f1b17f60f7080b/pkg/util/validation/validation.go#L198 |  | MaxLength: 253 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` <br />Required: \{\} <br /> |
| `reason` _string_ | reason provides a brief explanation of the evaluation result. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `message` _string_ | message is a human-readable message indicating details about the evaluation. |  | MaxLength: 10240 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `lastEvaluationTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#time-v1-meta)_ | lastEvaluationTime is the timestamp of the last rule check failed for this Node. |  | Required: \{\} <br /> |


#### NodeReadinessEvaluation



NodeReadinessEvaluation is the Schema for the NodeReadinessEvaluations API.
Each instance maps 1:1 to a Node and folds the outcomes of all applicable
NodeReadinessRules for that node into a single object.
An ownerReference to the corresponding Node is set for automatic garbage
collection when the node is deleted.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `readiness.node.x-k8s.io/v1alpha1` | | |
| `kind` _string_ | `NodeReadinessEvaluation` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[NodeReadinessEvaluationSpec](#nodereadinessevaluationspec)_ | spec defines the desired state of NodeReadinessEvaluation. |  | Required: \{\} <br /> |
| `status` _[NodeReadinessEvaluationStatus](#nodereadinessevaluationstatus)_ | status defines the observed state of NodeReadinessEvaluation. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### NodeReadinessEvaluationSpec



NodeReadinessEvaluationSpec defines the desired state of NodeReadinessEvaluation.



_Appears in:_
- [NodeReadinessEvaluation](#nodereadinessevaluation)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeName` _string_ | nodeName specifies the exact name of the target Kubernetes Node.<br />This object establishes a strict 1:1 relationship with the specified node,<br />acting as the single source of truth for all rules and statuses applied to it.<br />Because it binds this resource to a specific physical or virtual machine, it cannot be changed once set.<br />The validation constraints enforce standard Kubernetes resource naming<br />(RFC 1123 DNS Subdomain format), as defined in upstream apimachinery:<br />https://github.com/kubernetes/apimachinery/blob/master/pkg/util/validation/validation.go#L198 |  | MaxLength: 253 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` <br />Required: \{\} <br /> |


#### NodeReadinessEvaluationStatus



NodeReadinessEvaluationStatus defines the observed state of NodeReadinessEvaluation.

_Validation:_
- MinProperties: 1

_Appears in:_
- [NodeReadinessEvaluation](#nodereadinessevaluation)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#condition-v1-meta) array_ | conditions represent the latest available observations of the node's readiness evaluation state.<br />Known condition types are:<br />- "Evaluated": indicates whether the controller successfully evaluated all rules without errors.<br />- "Available": indicates whether the node has satisfied all rules, has zero taints applied, and is available for scheduling. |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `state` _[NodeEvaluationState](#nodeevaluationstate)_ | state indicates the overall readiness state of the node based on all applicable rules.<br />It acts as a top-level health indicator for this node's readiness evaluation. |  | Enum: [Available NotAvailable] <br />Optional: \{\} <br /> |
| `rules` _[RuleEvaluation](#ruleevaluation) array_ | rules contains the evaluation outcomes for all rules applicable to this node.<br />Each entry is keyed by ruleName, allowing independent per-rule updates by<br />parallel rule-workers without last-write-wins conflicts (listType=map). |  | MaxItems: 100 <br />Optional: \{\} <br /> |


#### NodeReadinessRule



NodeReadinessRule is the Schema for the NodeReadinessRules API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `readiness.node.x-k8s.io/v1alpha1` | | |
| `kind` _string_ | `NodeReadinessRule` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[NodeReadinessRuleSpec](#nodereadinessrulespec)_ | spec defines the desired state of NodeReadinessRule |  | Required: \{\} <br /> |
| `status` _[NodeReadinessRuleStatus](#nodereadinessrulestatus)_ | status defines the observed state of NodeReadinessRule |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### NodeReadinessRuleSpec



NodeReadinessRuleSpec defines the desired state of NodeReadinessRule.



_Appears in:_
- [NodeReadinessRule](#nodereadinessrule)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[ConditionRequirement](#conditionrequirement) array_ | conditions contains a list of the Node conditions that defines the specific<br />criteria that must be met for taints to be managed on the target Node.<br />The presence or status of these conditions directly triggers the application or removal of Node taints. |  | MaxItems: 32 <br />MinItems: 1 <br />Required: \{\} <br /> |
| `enforcementMode` _[EnforcementMode](#enforcementmode)_ | enforcementMode specifies how the controller maintains the desired state.<br />enforcementMode is one of bootstrap-only, continuous.<br />"bootstrap-only" applies the configuration once during initial setup.<br />"continuous" ensures the state is monitored and corrected throughout the resource lifecycle. |  | Enum: [bootstrap-only continuous] <br />Required: \{\} <br /> |
| `taint` _[Taint](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#taint-v1-core)_ | taint defines the specific Taint (Key, Value, and Effect) to be managed<br />on Nodes that meet the defined condition criteria.<br />The taint key must follow Kubernetes qualified name format: prefix/name<br />where prefix is 'readiness.k8s.io' (DNS subdomain) and name is a qualified<br />name (max 63 chars, alphanumeric, '-', '_', '.', must start and end with alphanumeric).<br />ref: git.k8s.io/kubernetes/staging/src/k8s.io/apimachinery/pkg/api/validate/content/kube.go#L24-L72<br />Supported effects: NoSchedule, PreferNoSchedule, NoExecute.<br />Caution: NoExecute evicts existing pods and can cause significant disruption<br />when combined with continuous enforcement mode. Prefer NoSchedule for most use cases. |  | Required: \{\} <br /> |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#labelselector-v1-meta)_ | nodeSelector limits the scope of this rule to a specific subset of Nodes.<br />An empty selector matches every Node in the cluster, so it is rejected.<br />At least one of matchLabels or matchExpressions must be set. |  | Required: \{\} <br /> |
| `conditionPolicy` _[ConditionPolicy](#conditionpolicy)_ | conditionPolicy controls how the conditions list is evaluated.<br />"allOf" (default) requires every condition to match its requiredStatus before the taint is removed.<br />"anyOf" requires at least one condition to match its requiredStatus.<br />anyOf cannot be used with enforcementMode: bootstrap-only. |  | Enum: [allOf anyOf] <br />Optional: \{\} <br /> |
| `dryRun` _boolean_ | dryRun when set to true, The controller will evaluate Node conditions and log intended taint modifications<br />without persisting changes to the cluster. Proposed actions are reflected in the resource status. |  | Optional: \{\} <br /> |


#### NodeReadinessRuleStatus



NodeReadinessRuleStatus defines the observed state of NodeReadinessRule.

_Validation:_
- MinProperties: 1

_Appears in:_
- [NodeReadinessRule](#nodereadinessrule)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `observedGeneration` _integer_ | observedGeneration reflects the generation of the most recently observed NodeReadinessRule by the controller. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `appliedNodes` _string array_ | appliedNodes lists the names of Nodes where the taint has been successfully managed.<br />This provides a quick reference to the scope of impact for this rule. |  | MaxItems: 5000 <br />items:MaxLength: 253 <br />Optional: \{\} <br /> |
| `failedNodes` _[NodeFailure](#nodefailure) array_ | failedNodes lists the Nodes where the rule evaluation encountered an error.<br />This is used for troubleshooting configuration issues, such as invalid selectors during node lookup. |  | MaxItems: 5000 <br />Optional: \{\} <br /> |
| `nodeEvaluations` _[NodeEvaluation](#nodeevaluation) array_ | nodeEvaluations provides detailed insight into the rule's assessment for individual Nodes.<br />This is primarily used for auditing and debugging why specific Nodes were or<br />were not targeted by the rule. |  | MaxItems: 5000 <br />Optional: \{\} <br /> |
| `dryRunResults` _[DryRunResults](#dryrunresults)_ | dryRunResults captures the outcome of the rule evaluation when DryRun is enabled.<br />This field provides visibility into the actions the controller would have taken,<br />allowing users to preview taint changes before they are committed. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### RuleEvaluation



RuleEvaluation defines the outcome of evaluating a single NodeReadinessRule against this Node.



_Appears in:_
- [NodeReadinessEvaluationStatus](#nodereadinessevaluationstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ruleName` _string_ | ruleName is the name of the NodeReadinessRule.<br />This field is the map key for status.rules (listType=map) and must always be present. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `ruleUID` _[UID](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#uid-types-pkg)_ | ruleUID is the UID of the NodeReadinessRule.<br />If the rule is deleted and recreated with the same name, the UID will differ,<br />allowing the controller to detect and replace stale evaluation entries. |  | Required: \{\} <br /> |
| `ruleStatus` _[RuleStatus](#rulestatus)_ | ruleStatus indicates the overall outcome of the rule's criteria against the Node. |  | Enum: [Satisfied Unsatisfied] <br />Required: \{\} <br /> |
| `taintStatus` _[TaintStatus](#taintstatus)_ | taintStatus reflects the observed state of the rule's specified taint on the Node (Present/Absent). |  | Enum: [Present Absent] <br />Required: \{\} <br /> |
| `taintKey` _string_ | taintKey is the key of the taint managed by this rule, stamped at evaluation<br />time so this entry is self-contained without requiring a lookup of the rule.<br />Matches rule.spec.taint.key. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `taintEffect` _[TaintEffect](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#tainteffect-v1-core)_ | taintEffect is the effect of the taint managed by this rule, stamped at<br />evaluation time so this entry is self-contained without requiring a lookup<br />of the rule. Matches rule.spec.taint.effect. |  | Enum: [NoSchedule PreferNoSchedule NoExecute] <br />Required: \{\} <br /> |
| `conditionPolicy` _[ConditionPolicy](#conditionpolicy)_ | conditionPolicy is the aggregation policy used when evaluating the conditions list,<br />stamped at evaluation time so this entry is self-contained without requiring a lookup<br />of the rule. Matches rule.spec.conditionPolicy. |  | Enum: [allOf anyOf] <br />Required: \{\} <br /> |
| `reason` _string_ | reason contains a concise, machine-readable string detailing the primary outcome. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `message` _string_ | message is a comprehensive, human-readable explanation providing further context. |  | MaxLength: 10240 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `readinessConditions` _[ConditionEvaluationResult](#conditionevaluationresult) array_ | readinessConditions provides a detailed breakdown of each condition evaluation<br />for this Node. This allows for granular debugging of which specific criteria passed/failed. |  | MaxItems: 32 <br />Optional: \{\} <br /> |
| `lastEvaluatedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#time-v1-meta)_ | lastEvaluatedAt records the exact moment the controller most recently assessed this rule. |  | Required: \{\} <br /> |
| `firstEvaluatedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#time-v1-meta)_ | firstEvaluatedAt is the time the rule was first assessed against this node. |  | Optional: \{\} <br /> |
| `taintObservedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#time-v1-meta)_ | taintObservedAt is the time NRC first observed the taint present on the node,<br />regardless of whether NRC applied it or it was pre-existing (e.g. via --register-with-taints).<br />This marks the beginning of the node being blocked by this rule and is the correct<br />start time for computing time-to-unblock SLIs. |  | Optional: \{\} <br /> |
| `taintAddedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#time-v1-meta)_ | taintAddedAt is the time NRC itself first applied the taint to the node in the<br />current taint lifecycle (i.e., since the last removal). This field is nil when<br />the taint was pre-existing and NRC adopted it rather than creating it.<br />The value is set once on the Absent→Present transition and carried forward on<br />subsequent reconciles; it is not the timestamp of the most recent reconcile.<br />Use taintObservedAt for "how long has the node been blocked"; use this field<br />to measure NRC's own apply latency (taintAddedAt - firstEvaluatedAt). |  | Optional: \{\} <br /> |
| `taintRemovedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#time-v1-meta)_ | taintRemovedAt is the time the controller successfully removed the taint<br />after the node satisfied all conditions. |  | Optional: \{\} <br /> |


#### RuleStatus

_Underlying type:_ _string_

RuleStatus defines the result of evaluating a NodeReadinessRule's criteria against a Node.
Rule-configuration faults (e.g. an invalid NodeSelector) are reported on the
NodeReadinessRule's own status conditions, not here.

_Validation:_
- Enum: [Satisfied Unsatisfied]

_Appears in:_
- [RuleEvaluation](#ruleevaluation)

| Field | Description |
| --- | --- |
| `Satisfied` | RuleStatusSatisfied indicates that the Node successfully met all conditions<br />defined in the NodeReadinessRule. The controller will ensure the corresponding<br />taint is removed so the node is unblocked.<br /> |
| `Unsatisfied` | RuleStatusUnsatisfied indicates that one or more conditions defined in the<br />NodeReadinessRule were not met. The controller will ensure the corresponding<br />taint remains present to block scheduling.<br /> |


#### TaintStatus

_Underlying type:_ _string_

TaintStatus specifies status of the Taint on Node.

_Validation:_
- Enum: [Present Absent]

_Appears in:_
- [NodeEvaluation](#nodeevaluation)
- [RuleEvaluation](#ruleevaluation)

| Field | Description |
| --- | --- |
| `Present` | TaintStatusPresent represent the taint present on the Node.<br /> |
| `Absent` | TaintStatusAbsent represent the taint absent on the Node.<br /> |


