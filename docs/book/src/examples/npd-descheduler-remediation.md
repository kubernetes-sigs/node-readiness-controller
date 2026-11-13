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

### 1. Choose Your Simulation Path

This guide supports two independent paths. **Do not mix them** — NPD continuously reconciles node conditions and will overwrite any manual patches you apply while it is running.

---

#### Path A — Real NPD Deployment (production-like)

Use this path to test the full automated loop end-to-end with an actual NPD DaemonSet reporting real node health.

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

Proceed with steps 2–4, then skip to [Path A Verification](#path-a-verification-npd-driven).

---

#### Path B — Manual Node Condition Patching (quick local simulation)

Use this path to simulate failures and recovery without deploying NPD. You will use `kubectl patch` commands in the [Verification](#path-b-verification-manual-simulation) section to drive condition changes directly.

> **Important**: If you previously deployed NPD (Path A), delete the NPD DaemonSet before proceeding:
> ```sh
> kubectl delete daemonset node-problem-detector -n kube-system
> ```

Skip the NPD deployment above and continue with steps 2–4.

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

**Check node taints at any point:**

```sh
kubectl get nodes -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints[*].key
```

---

### Path A Verification (NPD-driven)

With NPD running, node conditions are updated automatically based on the real health of the monitored component (e.g., the service on port 9100). No manual patching is needed or expected.

**1. Simulate component failure** by stopping the monitored service on a worker node. NPD will detect this within its configured polling interval and set `CustomCondition/MyComponentNotReady=True`.

**2. Observe taint applied by NRC:**

```sh
kubectl get node npd-descheduler-demo-worker -o jsonpath='{"\n"}{.spec.taints}{"\n"}'
```

**3. Observe pod eviction by Descheduler:**

The Descheduler scans every 30 seconds. Within a half-minute you will see the pod evicted and rescheduled:

```sh
kubectl get pods -o wide
kubectl get events --sort-by=.lastTimestamp | grep -i evict
```

**4. Simulate recovery** by restarting the monitored service. NPD will set the condition back to `False`, NRC will remove the taint, and the node becomes schedulable again:

```sh
kubectl get nodes -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints[*].key
```

---

### Path B Verification (Manual Simulation)

> **Important**: Ensure NPD is **not running** before proceeding. If it is, delete it first:
> ```sh
> kubectl delete daemonset node-problem-detector -n kube-system
> ```

**1. Simulate component recovery** (mark node as healthy so pods can schedule):

```sh
kubectl patch node npd-descheduler-demo-worker --type=strategic --subresource=status -p \
  '{"status":{"conditions":[{"type":"CustomCondition/MyComponentNotReady","status":"False","lastHeartbeatTime":"'$(date -u +%FT%TZ)'","lastTransitionTime":"'$(date -u +%FT%TZ)'"}]}}'
```

Wait a moment, then verify the pods have scheduled onto the node:

```sh
kubectl get pods -o wide
```

**2. Simulate a component failure:**

NRC will detect the condition change and add the taint:

```sh
kubectl patch node npd-descheduler-demo-worker --type=strategic --subresource=status -p \
  '{"status":{"conditions":[{"type":"CustomCondition/MyComponentNotReady","status":"True","lastHeartbeatTime":"'$(date -u +%FT%TZ)'","lastTransitionTime":"'$(date -u +%FT%TZ)'"}]}}'
```

**3. Observe taint applied by NRC:**

```sh
kubectl get node npd-descheduler-demo-worker -o jsonpath='{"\n"}{.spec.taints}{"\n"}'
```

**4. Observe pod eviction by Descheduler:**

The Descheduler scans every 30 seconds. Within a half-minute you will see the pod evicted and rescheduled:

```sh
kubectl get pods -o wide
kubectl get events --sort-by=.lastTimestamp | grep -i evict
```

**5. Simulate recovery:**

```sh
kubectl patch node npd-descheduler-demo-worker --type=strategic --subresource=status -p \
  '{"status":{"conditions":[{"type":"CustomCondition/MyComponentNotReady","status":"False","lastHeartbeatTime":"'$(date -u +%FT%TZ)'","lastTransitionTime":"'$(date -u +%FT%TZ)'"}]}}'
```

NRC removes the taint and the node becomes schedulable again.
