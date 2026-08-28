# CNI Readiness Example (Calico)

This example demonstrates using `readiness-condition-reporter` to monitor the Calico CNI health endpoint and publish the result as a Kubernetes node condition. Node Readiness Controller (NRC) watches the reported node condition and removes the startup taint once the configured NodeReadinessRule is satisfied.

NRC does not communicate directly with Calico or the reporter. It simply watches `node.status.conditions` and evaluates the configured readiness rule.

### How it works:
1. Nodes join with a `readiness.k8s.io/NetworkReady=pending:NoSchedule` taint.
2. A lightweight DaemonSet (`cni-reporter-ds.yaml`)
   monitors Calico's health endpoint (`localhost:9099/readiness`) and updates a
   node condition `projectcalico.org/CalicoReady`.
3. The `NodeReadinessRule` (`network-readiness-rule.yaml`) instructs the controller to remove the startup taint once the `projectcalico.org/CalicoReady` condition becomes `True`.
4. The reporter is deployed with `hostNetwork: true` to reach Calico's local health endpoint.
5. The reporter needs a dedicated ServiceAccount (`cni-reporter`) with permissions to patch node status.

## Files

| File | Description |
|---|---|
| `cni-reporter-ds.yaml` | `readiness-condition-reporter` DaemonSet that checks Calico's health endpoint and writes the node condition |
| `network-readiness-rule.yaml` | `NodeReadinessRule` that instructs NRC to manage the `readiness.k8s.io/NetworkReady` taint based on the `projectcalico.org/CalicoReady` condition |
| `network-readiness-dryrun-rule.yaml` | A dry-run variant of the rule for observing NRC behaviour without applying taints |
| `calico-rbac-node-status-patch-role.yaml` | RBAC permissions allowing the reporter's `ServiceAccount` to patch node status |
| `apply-calico.sh` | Script to deploy Calico with the reporter injected as a sidecar |
