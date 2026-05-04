# NPD + Descheduler Remediation

This guide demonstrates how to build an automated self-healing remediation loop using **Node Problem Detector (NPD)**, the **Node Readiness Controller (NRC)**, and the **Descheduler**.

## The Problem

When a node-level component fails (hardware driver, daemon, agent), existing pods continue running on that degraded node. Manual intervention is needed to identify the issue, taint the node, and reschedule workloads.

## The Solution

An automated remediation loop:

1. **NPD** runs a custom health check and sets a NodeCondition when a failure is detected.
2. **NRC** watches the condition and applies a taint to the unhealthy node.
3. **Descheduler** evicts pods that don't tolerate the taint.
4. The **Kubernetes Scheduler** reschedules evicted pods to healthy nodes.
5. When the issue recovers, NRC removes the taint automatically.

## Step-by-Step Guide

> **Note**: All manifests are available in the [`examples/npd-descheduler-remediation/`](../../../../examples/npd-descheduler-remediation) directory.

### Prerequisites

**1. Node Readiness Controller:**

Ensure the NRC is deployed. See the [Installation Guide](../user-guide/installation.md).

**2. Kind Cluster (for testing):**

```sh
kind create cluster --config examples/npd-descheduler-remediation/kind-cluster-config.yaml
```

This creates a cluster with 1 control-plane and 2 worker nodes. The workers are pre-tainted with `readiness.k8s.io/my-component-ready=false:NoSchedule` to represent starting in an "unknown" or initializing state.

### 1. (Optional) Deploy Node Problem Detector

> **Note**: For the verification section below, we will use manual patching to simulate failures. If you deploy NPD, it will overwrite manual patches every 10 seconds. You can skip this step or delete the NPD DaemonSet when you reach the verification steps.

NPD monitors node health with a custom plugin that checks a local component (e.g., a hardware driver listening on port 9100).

```sh
# Deploy NPD RBAC
kubectl apply -f examples/npd-descheduler-remediation/npd-rbac.yaml

# Deploy NPD config and DaemonSet
kubectl apply -f examples/npd-descheduler-remediation/npd-custom-plugin-config.yaml
kubectl apply -f examples/npd-descheduler-remediation/npd-daemonset.yaml
```

NPD sets the condition `CustomCondition/MyComponentNotReady`:
- `False` → component is healthy
- `True` → component has a problem

**Customizing the health check:** Edit `check-component.sh` in [`npd-custom-plugin-config.yaml`](../../../../examples/npd-descheduler-remediation/npd-custom-plugin-config.yaml) to check your actual component.

### 2. Create the NodeReadinessRule

```yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: my-component-readiness-rule
spec:
  conditions:
    - type: "CustomCondition/MyComponentNotReady"
      requiredStatus: "False"   # Remove taint when component is NOT unhealthy
  taint:
    key: "readiness.k8s.io/my-component-ready"
    effect: "NoSchedule"
    value: "false"
  enforcementMode: "continuous"  # Re-taint if component fails again
  nodeSelector:
    matchExpressions:
      - key: node-role.kubernetes.io/control-plane
        operator: DoesNotExist
```

Key points:
- **`continuous` mode** ensures the taint is re-applied if the component becomes unhealthy again — critical for the Descheduler to trigger pod eviction.
- The `nodeSelector` excludes the control-plane.

```sh
kubectl apply -f examples/npd-descheduler-remediation/node-readiness-rule.yaml
```

### 3. Deploy the Descheduler

The Descheduler runs with the `RemovePodsViolatingNodeTaints` strategy, scoped to our custom taint:

```yaml
profiles:
- name: default
  pluginConfig:
  - name: RemovePodsViolatingNodeTaints
    args:
      includedTaints:
      - "readiness.k8s.io/my-component-ready"
  plugins:
    deschedule:
      enabled:
      - RemovePodsViolatingNodeTaints
```

```sh
kubectl apply -f examples/npd-descheduler-remediation/descheduler-rbac.yaml
kubectl apply -f examples/npd-descheduler-remediation/descheduler-policy.yaml
kubectl apply -f examples/npd-descheduler-remediation/descheduler-deployment.yaml
```

### 4. Deploy a Sample Workload

Deploy a test workload *without* a toleration for the readiness taint:

```sh
kubectl apply -f examples/npd-descheduler-remediation/sample-workload.yaml
```

## Verification

**1. Check node conditions:**

```sh
kubectl get nodes -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints[*].key
```

**2. Simulate component recovery:**

First, let's mark the component as healthy so the initial taint is removed and our pods can schedule:

```sh
kubectl patch node npd-descheduler-demo-worker --type=strategic --subresource=status -p \
  '{"status":{"conditions":[{"type":"CustomCondition/MyComponentNotReady","status":"False","lastHeartbeatTime":"'$(date -u +%FT%TZ)'","lastTransitionTime":"'$(date -u +%FT%TZ)'"}]}}'
```

Wait a moment, then verify the pods have scheduled onto the node:

```sh
kubectl get pods -o wide
```

**3. Simulate a component failure:**

Now, let's simulate the component failing over time. NRC will detect this and add the taint.

```sh
kubectl patch node npd-descheduler-demo-worker --type=strategic --subresource=status -p \
  '{"status":{"conditions":[{"type":"CustomCondition/MyComponentNotReady","status":"True","lastHeartbeatTime":"'$(date -u +%FT%TZ)'","lastTransitionTime":"'$(date -u +%FT%TZ)'"}]}}'
```

**4. Observe taint applied by NRC:**

```sh
kubectl get node npd-descheduler-demo-worker -o jsonpath='{"\n"}{.spec.taints}{"\n"}'
```

**5. Observe pod eviction by Descheduler:**

Since the Descheduler scans every 30 seconds, within a half-minute you will see the pod evicted and rescheduled.

```sh
kubectl get pods -o wide   # The pod should be rescheduled away from the tainted node
kubectl get events --sort-by=.lastTimestamp | grep -i evict
```

**5. Simulate recovery:**

```sh
kubectl patch node <worker-node> --type=strategic --subresource=status -p \
  '{"status":{"conditions":[{"type":"CustomCondition/MyComponentNotReady","status":"False","lastHeartbeatTime":"'$(date -u +%FT%TZ)'","lastTransitionTime":"'$(date -u +%FT%TZ)'"}]}}'
```

The NRC removes the taint, and the node becomes schedulable again.
