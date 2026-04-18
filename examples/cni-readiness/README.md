# CNI Readiness Example (Calico)

This example demonstrates how to use the Node Readiness Controller to ensure nodes are only marked ready for workloads after the CNI (Calico) has fully initialized.

### How it works:
1. Nodes join with a `readiness.k8s.io/network/not-ready=pending:NoSchedule` taint.
2. A lightweight DaemonSet (`cni-reporter-ds.yaml`)
   monitors Calico's health endpoint (`localhost:9099/readiness`) and updates a
   node condition `projectcalico.org/CalicoReady`.
3. The `NodeReadinessRule` (`network-readiness-rule.yaml`) instructs the controller to remove the startup taint once the `projectcalico.org/CalicoReady` condition becomes `True`.
4. The reporter is deployed with `hostNetwork: true` to reach Calico's local health endpoint.
5. The reporter needs a dedicated ServiceAccount (`cni-reporter`) with permissions to patch node status.
