# Problem-Gate Readiness (defaultStatus)

This example demonstrates the `defaultStatus` field on a `ConditionRequirement`,
used to implement a "problem gate": a rule guarded by a problem-oriented
condition (in the style of [Node Problem Detector](https://github.com/kubernetes/node-problem-detector))
that should default to healthy when the condition is absent, rather than
blocking the node until the reporter has run at least once.

See the [Problem-Gate Readiness guide](https://kubernetes-sigs.github.io/node-readiness-controller/examples/problem-gate-readiness.html)
for the full walkthrough, and
[Core Concepts: Default Condition Status](https://kubernetes-sigs.github.io/node-readiness-controller/user-guide/concepts.html#default-condition-status-defaultstatus)
for background on `defaultStatus`.

## Files

- `npd-maintenance-config.yaml` — NPD custom-plugin ConfigMap reporting a
  `MaintenanceRequired` condition.
- `npd-maintenance-daemonset.yaml` — NPD DaemonSet running the plugin above.
- `npd-rbac.yaml` — ServiceAccount/ClusterRole/ClusterRoleBinding for the
  DaemonSet to patch Node status.
- `maintenance-readiness-rule.yaml` — `NodeReadinessRule` with
  `requiredStatus: "False"` and `defaultStatus: "False"` on
  `MaintenanceRequired`, `enforcementMode: "continuous"`.

## Apply

```sh
kubectl apply -f npd-rbac.yaml
kubectl apply -f npd-maintenance-config.yaml
kubectl apply -f npd-maintenance-daemonset.yaml
kubectl apply -f maintenance-readiness-rule.yaml
```

## Verify

A node with no `MaintenanceRequired` condition yet (NPD hasn't run, or hasn't
flagged anything) is not tainted:

```sh
kubectl get node <node-name> -o jsonpath='{.spec.taints}'
```

Simulate a maintenance flag on the node (this triggers the plugin's
`check-maintenance.sh` to exit non-zero) and confirm the taint is applied:

```sh
# on the node
sudo mkdir -p /var/run/maintenance && sudo touch /var/run/maintenance/scheduled

kubectl get node <node-name> -o jsonpath='{.status.conditions[?(@.type=="MaintenanceRequired")]}' | jq .
kubectl get node <node-name> -o jsonpath='{.spec.taints}'
```

Remove the flag and confirm the taint is removed again:

```sh
# on the node
sudo rm /var/run/maintenance/scheduled
```
