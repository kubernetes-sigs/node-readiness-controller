# Problem-Gate (Default-Allow)

This guide demonstrates how to configure the Node Readiness Controller (NRC) to implement a **problem gate** using a **default-allow** pattern.

## Background: Readiness Gating vs. Problem Gating

The Node Readiness Controller supports two primary architectural patterns for managing node readiness and health:

1. **Readiness-Gating (Default-Block)**:
   - Used for critical infrastructure dependencies (such as CNI plugins, storage drivers, or security agents).
   - The node is assumed **not ready** until the dependency explicitly reports `True` (e.g., `projectcalico.org/CalicoReady=True`).
   - If the condition is absent during node bootstrap, the controller evaluates it as `Unknown`, which does not satisfy the requirement, keeping the startup taint in place.

2. **Problem-Gating (Default-Allow)**:
   - Used for problem detectors (such as Node Problem Detector, GPU fault monitors, or custom hardware health checkers) that report unhealthy node conditions (e.g., `MaintenanceRequired=True` or `HardwareDegraded=True`).
   - Many problem detectors are designed to only publish a condition when an actual problem is detected; otherwise, the condition remains completely **absent** from the Node's status. Furthermore, during initial bootstrap, the detector DaemonSet itself may take time to start.
   - Using a **default-allow** configuration (`defaultStatus: "False"` with `requiredStatus: "False"`), NRC treats the missing condition as healthy (`False`), allowing the node to be untainted immediately.
   - If the problem detector later detects an issue and reports `MaintenanceRequired=True`, NRC's **continuous enforcement** re-applies the taint to isolate the node.

---

## The Solution: `defaultStatus`

By setting `defaultStatus: "False"` alongside `requiredStatus: "False"`, you tell NRC:
> *"Assume the node does not have this problem unless a problem detector explicitly reports otherwise."*

- **During Boot (Condition Absent)**: NRC resolves the missing condition to `False`, satisfying `requiredStatus: "False"`, and untaints the node immediately.
- **After Boot (Problem Detected)**: If the problem detector publishes `MaintenanceRequired=True`, the condition is no longer absent and evaluates to `True`. This breaks the rule expectation (`requiredStatus: "False"`), causing NRC to re-apply the taint.

---

## Step-by-Step Guide

> [!NOTE]
> You can find the example manifest in [`examples/problem-gate-readiness`](https://github.com/kubernetes-sigs/node-readiness-controller/tree/main/examples/problem-gate-readiness).

### 1. Bootstrap Nodes with a Startup Taint

Configure your nodes to join the cluster with a startup taint. For example:

```
readiness.k8s.io/problem-gate=pending:NoSchedule
```

### 2. Define the NodeReadinessRule

Create a `NodeReadinessRule` targeting the custom problem condition (e.g., `MaintenanceRequired`).

```yaml
# problem-gate-rule.yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: problem-gate-rule
spec:
  nodeSelector:
    matchLabels:
      kubernetes.io/os: linux
  conditions:
    - type: MaintenanceRequired
      requiredStatus: "False"
      defaultStatus: "False"  # Default-allow: treat absent condition as False (healthy)
  taint:
    key: readiness.k8s.io/problem-gate
    effect: NoSchedule
  enforcementMode: continuous
```

### 3. Apply and Verify

**Apply the rule:**
```bash
kubectl apply -f problem-gate-rule.yaml
```

**Verify rule evaluation:**
Check the rule status to confirm how NRC evaluates nodes with absent conditions:
```bash
kubectl get nodereadinessrule problem-gate-rule -o yaml
```

Output excerpt:
```yaml
status:
  nodeEvaluations:
    - nodeName: node-1
      satisfied: true
      conditionResults:
        - type: MaintenanceRequired
          currentStatus: Unknown      # Condition is absent from Node status
          requiredStatus: "False"
          defaultStatus: "False"      # Default status applied during evaluation
```

When a node joins, NRC immediately untaints it. If a monitoring tool later patches the node status to `MaintenanceRequired=True`, NRC will automatically re-taint the node to prevent scheduling.


