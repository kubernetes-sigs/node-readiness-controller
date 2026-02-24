# Getting Started

This guide covers how to use the Node Readiness Controller to define and enforce node readiness checks using `NodeReadinessRule` resources.

> **Prerequisites**: Node Readiness Controller must be installed. See [Installation](./installation.md).

## Creating a Readiness Rule

The core resource is the `NodeReadinessRule` CRD. It defines a set of conditions
that a node must meet to be considered "workload ready". If the conditions are
not met, the controller applies a specific taint to the node.

### Basic Example: Storage Readiness

Here is a rule that ensures a storage plugin is registered before allowing workloads that need it.

```yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: storage-readiness-rule
spec:
  # The label selector determines which nodes this rule applies to
  nodeSelector:
    matchLabels:
      storage-backend: "nfs"

  # The conditions that must be True for the node to be considered ready
  conditions:
    - type: "csi.example.com/NodePluginRegistered"
      requiredStatus: "True"
    - type: "csi.example.com/BackendReachable"
      requiredStatus: "True"

  # The taint to apply if conditions are NOT met
  taint:
    key: "readiness.k8s.io/vendor.com/nfs-unhealthy"
    effect: "NoSchedule"

  # When to enforce: 'bootstrap-only' (initial setup) or 'continuous' (ongoing health)
  enforcementMode: "continuous"
```

## Configuring the Rule

### 1. Select Target Nodes
Use the `nodeSelector` to target specific nodes (eg., GPU nodes).

> **Note**: These labels could be configured at node registration (e.g., via Kubelet `--node-labels`). Relying on labels added asynchronously by addons (like Node Feature Discovery) can create a race condition where the node remains schedulable until the labels appear.

### 2. Define Readiness Conditions
The `conditions` list defines the criteria. The controller watches the Node's status for these conditions.
*   `type`: The exact string matching the NodeCondition type.
*   `requiredStatus`: The status required (`True`, `False`, or `Unknown`).

### 3. Choose an Enforcement Mode
The `enforcementMode` determines how the controller manages the taint lifecycle.

*   **`bootstrap-only`**: Use this for one-time initialization tasks (e.g., installing a kernel module or driver). Once the conditions are met once, the taint is removed and never reapplied.
*   **`continuous`**: Use this for ongoing health checks (e.g., network connectivity). If the condition fails at any time, the taint is reapplied.

> For more details on these modes, see [Concepts](./concepts.md#enforcement-modes).

### 4. Configure the Taint
Define the taint that will block scheduling.
*   **Key**: Must start with `readiness.k8s.io/` prefix.
*   **Effect**:
    *   `NoSchedule`: Prevents new pods from scheduling (Recommended).
    *   `PreferNoSchedule`: Tries to avoid scheduling.
    *   `NoExecute`: Evicts running pods if they don't tolerate the taint.

> **Note**: To eliminate startup race conditions, register nodes with this taint (e.g., via Kubelet `--register-with-taints`). The controller will remove it once conditions are met.

> **Caution**: When using `NoExecute` with `continuous` mode: if a condition
> fails momentarily, all workloads on the node (without tolerations) will be
> immediately evicted, which can cause service disruption.


The admission webhook warns when using `NoExecute` taint:

```bash
$ kubectl apply -f rule.yaml
Warning: NOTE: NoExecute will evict existing pods without tolerations. Ensure critical system pods have appropriate tolerations
nodereadinessrule.readiness.node.x-k8s.io/my-rule created
```

### Rule Validations

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

#### Taint Key Naming
Taint keys must have the `readiness.k8s.io/` prefix to clearly identify
readiness-related taints and avoid conflicts with other controllers. 
Use unique, descriptive taint keys for different readiness checks. Follow [Kubernetes naming conventions](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/).


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


## Testing with Dry Run

You can preview the impact of a rule without actually tainting nodes using `dryRun`.

```yaml
spec:
  dryRun: true  # Enable dry run mode
  conditions:
    - type: "csi.example.com/NodePluginRegistered"
      requiredStatus: "True"
```

Check the `status` of the rule to follow the results:

```sh
kubectl get nodereadinessrule my-rule -o yaml
```

Look for `dryRunResults` in the output to see which nodes would be tainted.

## Reporting Node Conditions

The Node Readiness Controller only 'reacts' to observed conditions on the Node object. These conditions can be set by various tools:

1.  **Node Problem Detector (NPD)**: You can configure NPD with [custom plugins](https://github.com/kubernetes/node-problem-detector/blob/master/docs/custom_plugin_monitor.md) to monitor system state and report conditions.
2.  **Custom Health-Checkers or Sidecars**: You can run a daemonset or a small sidecar (eg., [Readiness Condition Reporter](../examples/security-agent-readiness.md#1-deploy-the-readiness-condition-reporter)) that checks your application or driver and updates the Node status.
3.  **External Controllers**: Any tool that can patch Node status can trigger these rules.

For a full example of setting up a custom condition for a security agent, see the [Security Agent Readiness Example](../examples/security-agent-readiness.md).
