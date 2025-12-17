
## Getting Started

### API Spec

#### Example: Storage Readiness Rule (Bootstrap-only)

This rule ensures nodes have working storage before removing the storage readiness taint:

```yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: storage-readiness-rule
spec:
  conditions:
    - type: "storage.kubernetes.io/CSIReady"
      requiredStatus: "True"
    - type: "storage.kubernetes.io/VolumePluginReady"
      requiredStatus: "True"
  taint:
    key: "readiness.k8s.io/StorageReady"
    effect: "NoSchedule"
    value: "pending"
  enforcementMode: "bootstrap-only"
  nodeSelector:
    matchLabels:
      storage-backend: "nfs"
  gracePeriod: "60s"
  dryRun: true  # Preview mode
```

#### NodeReadinessRule

| Field | Description | Required |
|-------|-------------|----------|
| `conditions` | List of node conditions that must ALL be satisfied | Yes |
| `conditions[].type` | Node condition type to evaluate | Yes |
| `conditions[].requiredStatus` | Required condition status (`True`, `False`, `Unknown`) | Yes |
| `taint.key` | Taint key to manage | Yes |
| `taint.effect` | Taint effect (`NoSchedule`, `PreferNoSchedule`, `NoExecute`) | Yes |
| `taint.value` | Optional taint value | No |
| `enforcementMode` | `bootstrap-only` or `continuous` | Yes |
| `nodeSelector` | Label selector to target specific nodes | No |
| `gracePeriod` | Grace period before applying taint changes | No |
| `dryRun` | Preview changes without applying them | No |

### Deployment

**Build and push your image to the location specified by `IMG_PREFIX`:`IMG_TAG` :**

```sh
make docker-build docker-push IMG_PREFIX=<some-registry>/nrr-controller IMG_TAG=tag
```

#### Option 1: Deploy Using Make Commands

```sh
# Install the CRDs
make install

# Deploy the controller
make deploy IMG_PREFIX=<some-registry>/nrr-controller IMG_TAG=tag

# Create sample rules
kubectl apply -k examples/network-readiness-rule.yaml
```

#### Option 2: Deploy Using Kustomize Directly

```sh
# Install CRDs
kubectl apply -k config/crd

# Deploy controller and RBAC
kubectl apply -k config/default

# Create sample rules
kubectl apply -f examples/network-readiness-rule.yaml
```

### Uninstallation

```sh
# Delete all rule instances
kubectl delete nodereadinessrules --all

# Delete the controller
make undeploy

# Delete the CRDs
make uninstall
```

## Operations

### Monitoring Rule Status

View rule status and evaluation results:

```sh
# List all rules
kubectl get nodereadinessrules

# Detailed status of a specific rule
kubectl describe nodereadinessrule network-readiness-rule

# Check rule evaluation per node
kubectl get nodereadinessrule network-readiness-rule -o yaml
```

The status includes:
- `appliedNodes`: Nodes this rule targets
- `failedNodes`: Nodes with evaluation errors
- `nodeEvaluations`: Per-node condition evaluation results
- `dryRunResults`: Impact analysis for dry-run rules

### Dry Run Mode

Test rules safely before applying:

```yaml
spec:
  dryRun: true  # Enable dry run mode
  conditions:
    - type: "storage.kubernetes.io/CSIReady"
      requiredStatus: "True"
  # ... rest of spec
```

Check dry run results:

```sh
kubectl get nodereadinessrule <rule-name> -o jsonpath='{.status.dryRunResults}'
```

### Enforcement Modes

#### Bootstrap-only Mode
- Removes bootstrap taint when conditions are first satisfied
- Marks completion with node annotation
- Stops monitoring after successful removal (fail-safe)
- Ideal for one-time setup conditions (storage, installing node daemons e.g: security agent or kernel-module update)

#### Continuous Mode
- Continuously monitors conditions
- Adds taint when any condition becomes unsatisfied
- Removes taint when all conditions become satisfied
- Ideal for ongoing health monitoring (network connectivity, resource availability)

## Configuration

### Security

The controller requires the following RBAC permissions:
- **Nodes**: `get`, `list`, `watch`, `patch`, `update` (for taint management)
- **NodeReadinessRules**: Full CRUD access
- **Events**: `create` (for status reporting)

### Performance and Scalability

- **Memory Usage**: ~64MB base + ~1KB per node + ~2KB per rule
- **CPU Usage**: Minimal during steady state, scales with node/rule change frequency
- **Node Scale**: Tested up to 100 nodes using kwok (1k nodes in progress)
- **Rule Scale**: Recommended maximum 50 rules per cluster

### Integration Patterns

#### With Node Problem Detector
```yaml
# custom NPD plugin checks and sets node conditions, controller manages taints
conditions:
  - type: "readiness.k8s.io/NetworkReady"  # Set by NPD
    requiredStatus: "False"
```

#### With Custom Health Checkers
```yaml
# Your daemonset sets custom conditions
conditions:
  - type: "readiness.k8s.io/mycompany.example.com/DatabaseReady"
    requiredStatus: "True"
  - type: "readiness.k8s.io/mycompany.example.com/CacheWarmed"
    requiredStatus: "True"
```

#### With Cluster Autoscaler
NodeReadinessController work well with cluster autoscaling:
- New nodes start with restrictive taints
- Controller removes taints once conditions are satisfied
- Autoscaler can safely scale knowing nodes are truly ready

## Development

### Building from Source

```sh
# Clone the repository
git clone https://sigs.k8s.io/node-readiness-controller.git
cd node-readiness-controller

# Run tests
make test

# Build binary
make build

# Generate manifests
make manifests
```

### Running Locally

```sh
# Install CRDs
make install

# Run against cluster (requires KUBECONFIG)
make run
```
