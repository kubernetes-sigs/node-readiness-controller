
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
| `dryRun` | Preview changes without applying them | No |

### Deployment

#### Option 1: Install official release images

Node-Readiness Controller offers two variants of the container image to support different cluster architectures.

Released container images are available for:
* **x86_64** (AMD64)
* **Arm64** (AArch64)

The controller image is available in the Kubernetes staging registry:

```sh
REPO="us-central1-docker.pkg.dev/k8s-staging-images/node-readiness-controller/node-readiness-controller"

TAG=$(skopeo list-tags docker://$REPO | jq .Tags[-1] | tr -d '"')

docker pull $REPO:$TAG
```

#### Option 2: Deploy Using Make Commands

**Build and push your image to the location specified by `IMG_PREFIX`:`IMG_TAG` :**

```sh
make docker-build docker-push IMG_PREFIX=<some-registry>/nrr-controller IMG_TAG=tag
```

```sh
# Install the CRDs
make install

# Deploy the controller
make deploy IMG_PREFIX=<some-registry>/nrr-controller IMG_TAG=tag

# Create sample rules
kubectl apply -k examples/network-readiness-rule.yaml
```

#### Option 3: Deploy Using Kustomize Directly

```sh
# Install CRDs
kubectl apply -k config/crd

# Deploy controller and RBAC
kubectl apply -k config/default

# Create sample rules
kubectl apply -f examples/network-readiness-rule.yaml
```

### Uninstallation

> **Important**: Follow this order to avoid stuck resources due to finalizers.

The controller adds a finalizer (`readiness.node.x-k8s.io/cleanup-taints`) to each `NodeReadinessRule` to ensure node taints are cleaned up before the rule is deleted. This means you must delete CRs **while the controller is still running**.

```sh
# 1. Delete all rule instances first (while controller is running)
kubectl delete nodereadinessrules --all

# 2. Delete the controller
make undeploy

# 3. Delete the CRDs
make uninstall
```

#### Recovering from Stuck Resources

If you deleted the controller before removing the CRs, the finalizer will block CR deletion. To recover, manually remove the finalizer:

```sh
kubectl patch nodereadinessrule <rule-name> -p '{"metadata":{"finalizers":[]}}' --type=merge
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
