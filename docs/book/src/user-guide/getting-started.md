
## Getting Started

This guide covers creating and configuring `NodeReadinessRule` resources.

> **Prerequisites**: Node Readiness Controller must be installed. See [Installation](./installation.md).

### API Spec

#### Example: Storage Readiness Rule (Bootstrap-only)

This rule ensures nodes have working storage before removing the storage readiness taint:

```yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: nfs-storage-readiness-rule
spec:
  conditions:  
    - type: "csi.example.net/NodePluginRegistered"
    requiredStatus: "True"
    - type: "csi.example.net/BackendReachable"
    requiredStatus: "True"
    - type: "DiskPressure"
    requiredStatus: "False"
  taint:
    key: "readiness.k8s.io/vendor.com/nfs-unhealthy"
    effect: "NoSchedule"
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

### Enforcement Modes

#### Bootstrap-only Mode
- Removes bootstrap taint when conditions are first satisfied
- Marks completion with node annotation
- Stops monitoring after successful removal (fail-safe)
- Ideal for one-time setup conditions (installing node daemons e.g: security agent or kernel-module update)

#### Continuous Mode
- Continuously monitors conditions
- Adds taint when any condition becomes unsatisfied
- Removes taint when all conditions become satisfied
- Ideal for ongoing health monitoring (network connectivity, resource availability)

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
    - type: "csi.example.net/NodePluginRegistered"
      requiredStatus: "True"
  # ... rest of spec
```

Check dry run results:

```sh
kubectl get nodereadinessrule <rule-name> -o jsonpath='{.status.dryRunResults}'
```

### Rule Validation and Constraints

#### NoExecute Taint Effect Warning

**`NoExecute` with `continuous` enforcement mode will evict existing workloads when conditions fail.**

If a readiness condition on the node is failing temporarily (eg., the component restarted), all pods without matching tolerations are immediately evicted from the node, if configured with a `NoExecute` taint. Use `NoSchedule` to prevent new scheduling without disrupting running workloads.

The admission webhook warns when using `NoExecute`:

```sh
# NoExecute + continuous enforcement
$ kubectl apply -f rule.yaml
Warning: CAUTION: NoExecute with continuous mode evicts pods when conditions fail, risking workload disruption. Consider NoSchedule or bootstrap-only
nodereadinessrule.readiness.node.x-k8s.io/my-rule created

# NoExecute + bootstrap-only enforcement
$ kubectl apply -f rule.yaml
Warning: NOTE: NoExecute will evict existing pods without tolerations. Ensure critical system pods have appropriate tolerations
nodereadinessrule.readiness.node.x-k8s.io/my-rule created
```

See [Kubernetes taints documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for taint behavior details.

#### Avoiding Taint Key Conflicts

The admission webhook prevents multiple rules from using the same `taint.key` and `taint.effect` on overlapping node selectors.

**Example conflict:**
```yaml
# Rule 1
spec:
  conditions:
    - type: "device.gpu-vendor.net/DevicePluginRegistered"
      requiredStatus: "True"
  nodeSelector:
    matchLabels:
      feature.node.kubernetes.io/pci-10de.present: "true"
  taint:
    key: "readiness.k8s.io/vendor.com/gpu-not-ready"
    effect: "NoSchedule"

# Rule 2 - This will be REJECTED
spec:
  conditions:
    - type: "cniplugin.example.net/rdma/NetworkReady"
    requiredStatus: "True"
  nodeSelector:
    matchLabels:
      feature.node.kubernetes.io/pci-10de.present: "true"
  taint:
    key: "readiness.k8s.io/vendor.com/gpu-not-ready"  # Same (taint-key + effect) but different conditions = conflict
    effect: "NoSchedule"
```

Use unique, descriptive taint keys for different readiness checks.

#### Taint Key Naming

Follow [Kubernetes naming conventions](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/).

Taint keys must have the `readiness.k8s.io/` prefix to clearly identify readiness-related taints and avoid conflicts with other controllers

**Valid:**
```yaml
taint:
  key: "readiness.k8s.io/vendor.com/network-not-ready"
  key: "readiness.k8s.io/vendor.com/gpu-not-ready"
```

**Invalid:**
```yaml
taint:
  key: "network-ready"              # Missing prefix
  key: "node.kubernetes.io/ready"   # Wrong prefix
```


## Configuration

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
