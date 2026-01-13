# API Reference

## Packages
- [readiness.node.x-k8s.io/v1alpha1](#readinessnodex-k8siov1alpha1)


## readiness.node.x-k8s.io/v1alpha1

Package v1alpha1 contains API Schema definitions for the  v1alpha1 API group.

### Resource Types
- [NodeReadinessRule](#nodereadinessrule)



#### ConditionEvaluationResult



ConditionEvaluationResult provides a detailed report of the comparison between
the Node's observed condition and the rule's requirement.



_Appears in:_
- [NodeEvaluation](#nodeevaluation)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | type corresponds to the Node condition type being evaluated. |  | MaxLength: 316 <br />MinLength: 1 <br /> |
| `currentStatus` _[ConditionStatus](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#conditionstatus-v1-core)_ | currentStatus is the actual status value observed on the Node, one of True, False, Unknown. |  | Enum: [True False Unknown] <br /> |
| `requiredStatus` _[ConditionStatus](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#conditionstatus-v1-core)_ | requiredStatus is the status value defined in the rule that must be matched, one of True, False, Unknown. |  | Enum: [True False Unknown] <br /> |


#### ConditionRequirement



ConditionRequirement defines a specific Node condition and the status value
required to trigger the controller's action.



_Appears in:_
- [NodeReadinessRuleSpec](#nodereadinessrulespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | type of Node condition<br />Following kubebuilder validation is referred from https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Condition |  | MaxLength: 316 <br />MinLength: 1 <br /> |
| `requiredStatus` _[ConditionStatus](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#conditionstatus-v1-core)_ | requiredStatus is status of the condition, one of True, False, Unknown. |  | Enum: [True False Unknown] <br /> |


#### DryRunResults



DryRunResults provides a summary of the actions the controller would perform if DryRun mode is enabled.

_Validation:_
- MinProperties: 1

_Appears in:_
- [NodeReadinessRuleStatus](#nodereadinessrulestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `affectedNodes` _integer_ | affectedNodes is the total count of Nodes that match the rule's criteria. |  | Minimum: 0 <br /> |
| `taintsToAdd` _integer_ | taintsToAdd is the number of Nodes that currently lack the specified taint and would have it applied. |  | Minimum: 0 <br /> |
| `taintsToRemove` _integer_ | taintsToRemove is the number of Nodes that currently possess the<br />taint but no longer meet the criteria, leading to its removal. |  | Minimum: 0 <br /> |
| `riskyOperations` _integer_ | riskyOperations represents the count of Nodes where required conditions<br />are missing entirely, potentially indicating an ambiguous node state. |  | Minimum: 0 <br /> |
| `summary` _string_ | summary provides a human-readable overview of the dry run evaluation,<br />highlighting key findings or warnings. |  | MaxLength: 4096 <br />MinLength: 1 <br /> |


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
| `nodeName` _string_ | nodeName is the name of the evaluated Node. |  | MaxLength: 253 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` <br /> |
| `conditionResults` _[ConditionEvaluationResult](#conditionevaluationresult) array_ | conditionResults provides a detailed breakdown of each condition evaluation<br />for this Node. This allows for granular auditing of which specific<br />criteria passed or failed during the rule assessment. |  | MaxItems: 5000 <br /> |
| `taintStatus` _[TaintStatus](#taintstatus)_ | taintStatus represents the taint status on the Node, one of Present, Absent. |  | Enum: [Present Absent] <br /> |
| `lastEvaluationTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#time-v1-meta)_ | lastEvaluationTime is the timestamp when the controller last assessed this Node. |  |  |


#### NodeFailure



NodeFailure provides diagnostic details for Nodes that could not be successfully evaluated by the rule.



_Appears in:_
- [NodeReadinessRuleStatus](#nodereadinessrulestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeName` _string_ | nodeName is the name of the failed Node.<br />Following kubebuilder validation is referred from<br />https://github.com/kubernetes/apimachinery/blob/84d740c9e27f3ccc94c8bc4d13f1b17f60f7080b/pkg/util/validation/validation.go#L198 |  | MaxLength: 253 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` <br /> |
| `reason` _string_ | reason provides a brief explanation of the evaluation result. |  | MaxLength: 256 <br />MinLength: 1 <br /> |
| `message` _string_ | message is a human-readable message indicating details about the evaluation. |  | MaxLength: 10240 <br />MinLength: 1 <br /> |
| `lastEvaluationTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#time-v1-meta)_ | lastEvaluationTime is the timestamp of the last rule check failed for this Node. |  |  |


#### NodeReadinessRule



NodeReadinessRule is the Schema for the NodeReadinessRules API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `readiness.node.x-k8s.io/v1alpha1` | | |
| `kind` _string_ | `NodeReadinessRule` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[NodeReadinessRuleSpec](#nodereadinessrulespec)_ | spec defines the desired state of NodeReadinessRule |  |  |
| `status` _[NodeReadinessRuleStatus](#nodereadinessrulestatus)_ | status defines the observed state of NodeReadinessRule |  | MinProperties: 1 <br /> |


#### NodeReadinessRuleSpec



NodeReadinessRuleSpec defines the desired state of NodeReadinessRule.



_Appears in:_
- [NodeReadinessRule](#nodereadinessrule)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[ConditionRequirement](#conditionrequirement) array_ | conditions contains a list of the Node conditions that defines the specific<br />criteria that must be met for taints to be managed on the target Node.<br />The presence or status of these conditions directly triggers the application or removal of Node taints. |  | MaxItems: 32 <br />MinItems: 1 <br /> |
| `enforcementMode` _[EnforcementMode](#enforcementmode)_ | enforcementMode specifies how the controller maintains the desired state.<br />enforcementMode is one of bootstrap-only, continuous.<br />"bootstrap-only" applies the configuration once during initial setup.<br />"continuous" ensures the state is monitored and corrected throughout the resource lifecycle. |  | Enum: [bootstrap-only continuous] <br /> |
| `taint` _[Taint](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#taint-v1-core)_ | taint defines the specific Taint (Key, Value, and Effect) to be managed<br />on Nodes that meet the defined condition criteria. |  |  |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#labelselector-v1-meta)_ | nodeSelector limits the scope of this rule to a specific subset of Nodes. |  |  |
| `dryRun` _boolean_ | dryRun when set to true, The controller will evaluate Node conditions and log intended taint modifications<br />without persisting changes to the cluster. Proposed actions are reflected in the resource status. |  |  |


#### NodeReadinessRuleStatus



NodeReadinessRuleStatus defines the observed state of NodeReadinessRule.

_Validation:_
- MinProperties: 1

_Appears in:_
- [NodeReadinessRule](#nodereadinessrule)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `observedGeneration` _integer_ | observedGeneration reflects the generation of the most recently observed NodeReadinessRule by the controller. |  | Minimum: 1 <br /> |
| `appliedNodes` _string array_ | appliedNodes lists the names of Nodes where the taint has been successfully managed.<br />This provides a quick reference to the scope of impact for this rule. |  | MaxItems: 5000 <br />items:MaxLength: 253 <br /> |
| `failedNodes` _[NodeFailure](#nodefailure) array_ | failedNodes lists the Nodes where the rule evaluation encountered an error.<br />This is used for troubleshooting configuration issues, such as invalid selectors during node lookup. |  | MaxItems: 5000 <br /> |
| `nodeEvaluations` _[NodeEvaluation](#nodeevaluation) array_ | nodeEvaluations provides detailed insight into the rule's assessment for individual Nodes.<br />This is primarily used for auditing and debugging why specific Nodes were or<br />were not targeted by the rule. |  | MaxItems: 5000 <br /> |
| `dryRunResults` _[DryRunResults](#dryrunresults)_ | dryRunResults captures the outcome of the rule evaluation when DryRun is enabled.<br />This field provides visibility into the actions the controller would have taken,<br />allowing users to preview taint changes before they are committed. |  | MinProperties: 1 <br /> |


#### TaintStatus

_Underlying type:_ _string_

TaintStatus specifies status of the Taint on Node.

_Validation:_
- Enum: [Present Absent]

_Appears in:_
- [NodeEvaluation](#nodeevaluation)

| Field | Description |
| --- | --- |
| `Present` | TaintStatusPresent represent the taint present on the Node.<br /> |
| `Absent` | TaintStatusAbsent represent the taint absent on the Node.<br /> |


