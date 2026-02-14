# Kubernetes Node Readiness Gates Project Context

## Project Overview

This project implements a Kubernetes controller that manages node taints based on readiness gate rules. It provides a flexible system for controlling when nodes are ready to accept workloads by monitoring multiple node conditions and applying/removing taints accordingly.

## Architecture

### Core Components

1. **NodeReadinessRule CRD**: Defines rules mapping multiple node conditions to a single taint
2. **ReadinessGateController**: Main controller that processes rules and manages node taints  
3. **NPD Integration**: Works with Node Problem Detector for condition monitoring
4. **Validation Webhook**: Prevents conflicting rule configurations

### Key Features

- **Multi-condition Rules**: Single taint controlled by multiple node conditions (ALL must be satisfied)
- **Bootstrap vs Continuous**: Rules can be bootstrap-only (remove taint once, then stop) or continuous (keep enforcing)
- **Dry Run Mode**: Preview rule impact before applying changes
- **Node Selector Support**: Rules can target specific node types
- **Conflict Prevention**: Validation prevents multiple rules targeting same taint key
- **Status Tracking**: Comprehensive observability of rule evaluation and node failures

## Directory Structure (Kubebuilder project)

```
├── api/v1alpha1/
│   ├── nodereadinessrule_types.go  # CRD type definitions  
│   ├── groupversion_info.go            # API version info
│   └── zz_generated.deepcopy.go        # Generated deep copy methods
├── internal/controller/
│   ├── nodereadinessrule_controller.go  # Rule reconciler
│   ├── node_controller.go                   # Node reconciler  
│   ├── nodereadinessrule_controller_test.go  # Rule controller tests
│   ├── node_controller_test.go              # Node controller tests
│   └── suite_test.go                        # Test suite setup
├── cmd/
│   └── main.go                         # Controller entrypoint
├── config/
│   ├── crd/bases/
│   │   └── readiness.node.x-k8s.io_nodereadinessrules.yaml  # Generated CRD
│   ├── rbac/
│   │   ├── role.yaml                   # Controller RBAC
│   │   ├── role_binding.yaml           # RBAC binding
│   │   └── service_account.yaml        # Service account
│   ├── manager/
│   │   └── manager.yaml                # Controller deployment
│   ├── samples/
│   │   └── v1alpha1_nodereadinessrule.yaml  # Example rule
│   └── default/
│       └── kustomization.yaml          # Default kustomize config
├── test/
│   ├── e2e/                           # End-to-end tests
│   └── utils/                         # Test utilities
├── hack/
│   └── boilerplate.go.txt             # Code generation header
├── Makefile                           # Build and deployment targets
├── Dockerfile                         # Container image build
└── PROJECT                            # Kubebuilder project config
```

## File Mappings (Kubebuilder vs Generic Structure)

### Controller Implementation

Files: internal/controller/nodereadinessrule_controller.go + internal/controller/node_controller.go
Content: Controller logic split into two files - rule reconciler and node reconciler

### Main Entry

Controller logic start here: cmd/main.go

### Generated CRD

Kubebuilder generated schema: config/crd/bases/readiness.node.x-k8s.io_nodereadinessrules.yaml

### RBAC

generated: config/rbac/*.yaml (multiple files via kubebuilder)

## Kubebuilder Integration Points

### Code Generation

* Uses controller-gen for CRD and RBAC generation
* Run make generate to update generated code
* Run make manifests to update CRD and RBAC files

### Testing Framework

* `internal/controller/suite_test.go` sets up test environment
* Integration with envtest for controller testing
* `test/e2e/` for end-to-end testing with real cluster

### Build and Deploy

* `make build` - builds controller binary
* `make docker-build` - builds container image
* `make deploy` - deploys to current kubectl context
* `make undeploy` - removes deployment

### Development Workflow

* Modify types in `api/v1alpha1/nodereadinessrule_types.go`
* Run `make generate manifests` to update generated files
* Updated controller logic in `internal/controller/*.go`
* Test with `make test`
* Deploy with `make deploy`

## Key Types and Data Structures

### NodeReadinessRule CRD

```go
type NodeReadinessRuleSpec struct {
    // Multiple conditions that must ALL be satisfied
    Conditions []ConditionRequirement `json:"conditions"`
    
    // Enforcement mode: "bootstrap-only" or "continuous"
    EnforcementMode EnforcementMode `json:"enforcementMode"`
    
    // Target taint specification
    Taint TaintSpec `json:"taint"`
    
    // Node selector for targeting specific nodes
    NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`
    
    // Grace period before applying taint changes
    GracePeriod *metav1.Duration `json:"gracePeriod,omitempty"`
    
    // Dry run mode - preview changes without applying
    DryRun bool `json:"dryRun,omitempty"`
}

type ConditionRequirement struct {
    Type           string                    `json:"type"`
    RequiredStatus corev1.ConditionStatus   `json:"requiredStatus"`
}

type TaintSpec struct {
    Key    string                `json:"key"`
    Effect corev1.TaintEffect    `json:"effect"`
    Value  string               `json:"value,omitempty"`
}

type EnforcementMode string
const (
    EnforcementModeBootstrapOnly EnforcementMode = "bootstrap-only"
    EnforcementModeContinuous    EnforcementMode = "continuous"
)
```

### Status Tracking

```go
type NodeReadinessRuleStatus struct {
    ObservedGeneration int64              `json:"observedGeneration,omitempty"`
    Conditions         []metav1.Condition `json:"conditions,omitempty"`
    AppliedNodes       []string           `json:"appliedNodes,omitempty"`
    CompletedNodes     []string           `json:"completedNodes,omitempty"` // For bootstrap-only
    FailedNodes        []NodeFailure      `json:"failedNodes,omitempty"`
    NodeEvaluations    []NodeEvaluation   `json:"nodeEvaluations,omitempty"`
    DryRunResults      *DryRunResults     `json:"dryRunResults,omitempty"`
}

type NodeEvaluation struct {
    NodeName          string                        `json:"nodeName"`
    ConditionResults  []ConditionEvaluationResult  `json:"conditionResults"`
    TaintAction       string                       `json:"taintAction"` // "add", "remove", "none"
    LastEvaluated     metav1.Time                  `json:"lastEvaluated"`
}

type ConditionEvaluationResult struct {
    Type           string                    `json:"type"`
    CurrentStatus  corev1.ConditionStatus   `json:"currentStatus"`
    RequiredStatus corev1.ConditionStatus   `json:"requiredStatus"`
    Satisfied      bool                     `json:"satisfied"`
    Missing        bool                     `json:"missing"`
}
```

## Controller Architecture

### Reconciler Structure

The controller uses multiple reconcilers:

1. **RuleReconciler**: Handles NodeReadinessRule changes
   - Updates rule cache
   - Processes dry run evaluations
   - Re-evaluates all applicable nodes when rules change
   
2. **NodeReconciler**: Handles node changes 
   - Processes new nodes
   - Handles condition updates
   - Evaluates node against all applicable rules

### Key Controller Methods

- `processNodeAgainstAllRules()`: Evaluates single node against all rules
- `processAllNodesForRule()`: Re-evaluates all nodes when rule changes
- `evaluateRuleForNode()`: Core evaluation logic for rule + node combination
- `processDryRun()`: Simulates rule impact without making changes
- Bootstrap completion tracking via node annotations

## Operational Modes

### Bootstrap-Only Mode
- Remove taint when conditions are first satisfied
- Mark completion with node annotation `readiness.k8s.io/bootstrap-completed-<ruleName>=true`
- Stop monitoring this node+rule combination until node restart/rejoin
- Ignore subsequent condition changes (fail-safe for race conditions)

### Continuous Mode
- Continuously monitor conditions and enforce taint state
- Add taint when any condition becomes unsatisfied
- Remove taint when all conditions become satisfied
- No completion tracking needed

## Rule Evaluation Logic

### Condition Aggregation
- **ALL logic**: All conditions must have `requiredStatus` to remove taint
- **Missing conditions**: Treated as `ConditionUnknown` (unsatisfied)
- **Evaluation order**: All conditions evaluated, no short-circuiting

### Taint Management
```go
shouldRemoveTaint := allConditionsSatisfied
currentlyHasTaint := hasTaintBySpec(node, rule.Spec.Taint)

if shouldRemoveTaint && currentlyHasTaint {
    // Remove taint - conditions satisfied
    action = "remove"
} else if !shouldRemoveTaint && !currentlyHasTaint {
    // Add taint - conditions not satisfied  
    action = "add"
} else {
    // No change needed
    action = "none"
}
```

## Example Configurations

### CNI Readiness Rule
```yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: cni-readiness-rule
spec:
  conditions:
    - type: "network.kubernetes.io/CNIReady"
      requiredStatus: "True"
    - type: "network.kubernetes.io/NetworkProxyReady"
      requiredStatus: "True"
  taint:
    key: "readiness.k8s.io/NetworkReady"
    effect: "NoSchedule"
    value: "pending"
  enforcementMode: "bootstrap-only"
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/worker: ""
```

### Storage Readiness Rule
```yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: storage-readiness-rule
spec:
  conditions:
    - type: "storage.kubernetes.io/CSIReady"
      requiredStatus: "True"
  taint:
    key: "readiness.k8s.io/StorageReady"
    effect: "NoSchedule"
  enforcementMode: "continuous"
  gracePeriod: "60s"
  dryRun: true  # Preview mode
```

## Integration Points

### NPD (Node Problem Detector) Integration
- NPD plugins update node conditions (e.g., `network.kubernetes.io/CNIReady`)
- Controller watches condition changes and evaluates rules
- Supports custom NPD plugins for domain-specific health checks

### Kubelet Integration
- Kubelet can add bootstrap taints on startup
- Bootstrap taints use same keys as managed taints
- Controller takes over taint management once rules are applied

### kubectl Integration
- Custom resource with proper printer columns
- Status provides comprehensive observability
- Dry run mode for safe rule testing

## Error Handling and Safety

### Validation
- CRD schema validation for basic type safety
- Admission webhook prevents conflicting rules (same taint key)
- Node selector overlap detection
- Kubernetes taint key naming convention validation

### Error Recovery
- Failed node evaluations recorded in rule status
- Controller continues processing other nodes on individual failures
- Conservative approach: missing conditions = unsatisfied (keep restrictive taints)

### Observability
- Comprehensive status tracking per rule and per node
- Condition evaluation results with timestamps
- Failed node tracking with detailed error messages
- Dry run impact analysis

## Performance Considerations

### Caching Strategy
- In-memory rule cache with RWMutex protection
- Efficient node selector evaluation
- Minimal API calls through controller-runtime client caching

### Scalability
- Separate reconcilers for rules vs nodes to avoid cross-dependencies
- Rate-limited work queues prevent API server overload
- Batch processing for multiple rule changes

### Resource Management
- Lightweight controller with minimal memory footprint
- Efficient informer-based event handling
- No persistent state storage (rebuilt from cluster state)

## Security Model

### RBAC Requirements
```yaml
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list", "watch", "patch", "update"]
- apiGroups: ["readiness.k8s.io"]
  resources: ["nodereadinessrules"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["readiness.k8s.io"]
  resources: ["nodereadinessrules/status"]
  verbs: ["get", "update", "patch"]
```

### Security Context
- Non-root user (UID 65532)
- Read-only root filesystem
- Minimal capabilities (drop ALL)
- No privilege escalation

## Testing Strategy

### Unit Testing
- Rule evaluation logic
- Condition aggregation
- Bootstrap completion tracking
- Dry run calculations

### Integration Testing
- Kind cluster deployment
- NPD integration testing
- Multi-rule conflict scenarios
- Bootstrap vs continuous mode validation

### End-to-End Testing
- Real cluster deployment
- Workload scheduling impact verification
- Performance under scale (x100, x1k, x5k nodes)
- Error recovery scenarios

## Future Extensions

### Planned Features (via nrrctl)
- Interactive rule management
- Hard rollback capabilities
- Rule version control and history
- Advanced conflict resolution

### Possible Enhancements
- Integration with cluster autoscaling
- Metrics and alerting integration
- Custom condition logic (custom expressions)
- Weighted conditions with priorities
- Time-based rule activation

## Dependencies

### Runtime Dependencies
- controller-runtime v0.15+
- client-go v0.28+
- Kubernetes 1.25+ (for CRD features)

### Optional Integrations
- Node Problem Detector v0.8+
- Prometheus (for metrics)
- Grafana (for dashboards)

## Deployment Considerations

### High Availability
- Single replica with leader election
- Stateless design enables easy failover
- Fast startup and cache rebuild

### Resource Requirements
- CPU: 100m request, 500m limit
- Memory: 128Mi request, 256Mi limit

### Networking
- Cluster internal only
- Health check endpoints on :8081
- Metrics endpoint on :8080