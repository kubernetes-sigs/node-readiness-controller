# Problem-Gate Readiness (`defaultStatus`)

This guide demonstrates how to use the `defaultStatus` field on a
`ConditionRequirement` to build a **problem gate**: a rule guarded by a
problem-oriented Node condition — reported in the style of
[Node Problem Detector (NPD)](https://github.com/kubernetes/node-problem-detector)
— that should default to healthy when the condition hasn't been reported yet,
rather than blocking the node.

## The Problem

Problem-oriented conditions (like `MemoryPressure`, `DiskPressure`, or a
custom `MaintenanceRequired` condition reported by NPD) are only ever written
to a Node's status *when a problem is detected*. On a freshly bootstrapped
node, NPD may not have run yet, so the condition is completely absent from
`status.conditions` — not `False`, just missing.

By default, the Node Readiness Controller resolves an absent condition to
`Unknown`. If a rule requires `requiredStatus: "False"` on that condition,
`Unknown` does not satisfy it, and the node stays tainted until NPD reports
the condition at least once. For a problem gate, that's backwards: the
absence of a problem report should mean "no known problem," not "blocked."

## The Solution

Set `defaultStatus: "False"` alongside `requiredStatus: "False"` on the
guarded condition:

- If the condition is **absent**, it evaluates to `False` (the configured
  default) — satisfying `requiredStatus: "False"` — so the node is not
  gated by the reporter's startup race.
- If NPD later reports the condition as `True` (a problem was detected), the
  rule is no longer satisfied and the taint is (re-)applied.
- The controller still reports the *observed* status (`Unknown` when
  absent) separately in `status.nodeEvaluations[].conditionResults[].currentStatus`,
  so you retain full visibility into whether a value came from the Node or
  from the configured default.

See [Core Concepts: Default Condition Status](../user-guide/concepts.md#default-condition-status-defaultstatus)
for the full semantics, including why `defaultStatus` is rejected by the
validating webhook when `enforcementMode` is `bootstrap-only`.

> [!NOTE]
> All manifests referenced in this guide are available in the
> [`examples/problem-gate-readiness`](https://github.com/kubernetes-sigs/node-readiness-controller/tree/main/examples/problem-gate-readiness)
> directory.

### Prerequisites

Before starting, ensure the Node Readiness Controller is deployed. See the
[Installation Guide](../user-guide/installation.md) for details.

### 1. Deploy the Problem Reporter (Node Problem Detector)

Deploy NPD with a custom plugin that reports a `MaintenanceRequired`
condition, defaulting to healthy (`False`) unless a maintenance flag file is
present on the node:

```yaml
# npd-maintenance-config.yaml (excerpt)
maintenance-plugin.json: |
  {
    "plugin": "custom",
    "source": "maintenance-monitor",
    "conditions": [
      { "type": "MaintenanceRequired", "reason": "NoMaintenanceScheduled", ... }
    ],
    "rules": [
      { "type": "permanent", "condition": "MaintenanceRequired", "path": "/config/plugin/check-maintenance.sh" }
    ]
  }
```

```sh
kubectl apply -f examples/problem-gate-readiness/npd-rbac.yaml
kubectl apply -f examples/problem-gate-readiness/npd-maintenance-config.yaml
kubectl apply -f examples/problem-gate-readiness/npd-maintenance-daemonset.yaml
```

### 2. Create the Node Readiness Rule

```yaml
# maintenance-readiness-rule.yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: maintenance-problem-gate-rule
spec:
  conditions:
    - type: "MaintenanceRequired"
      requiredStatus: "False"
      defaultStatus: "False"

  taint:
    key: "readiness.k8s.io/problem-gate"
    effect: "NoSchedule"

  # defaultStatus is not supported with "bootstrap-only" rules.
  enforcementMode: "continuous"

  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/worker: ""
```

```sh
kubectl apply -f examples/problem-gate-readiness/maintenance-readiness-rule.yaml
```

## Verification

1. **Before NPD has reported anything**, confirm the node is not tainted even
   though `MaintenanceRequired` is absent from its conditions:

   ```sh
   kubectl get node <node-name> -o jsonpath='{.status.conditions[?(@.type=="MaintenanceRequired")]}'
   kubectl get node <node-name> -o jsonpath='{.spec.taints}'
   ```

2. **Flag the node for maintenance** (simulated by creating a file the
   plugin's check script looks for) and confirm the taint is applied:

   ```sh
   # on the node
   sudo mkdir -p /var/run/maintenance && sudo touch /var/run/maintenance/scheduled
   ```

   ```sh
   kubectl get node <node-name> -o jsonpath='{.status.conditions[?(@.type=="MaintenanceRequired")]}' | jq .
   kubectl get node <node-name> -o jsonpath='{.spec.taints}'
   ```

   You should see `MaintenanceRequired=True` and the
   `readiness.k8s.io/problem-gate=NoSchedule` taint applied.

3. **Clear the flag** and confirm the taint is removed again:

   ```sh
   # on the node
   sudo rm /var/run/maintenance/scheduled
   ```
