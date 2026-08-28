# Security Agent Readiness

This example demonstrates two independent approaches for reporting the readiness of a security agent (Falco) to Node Readiness Controller (NRC).

In both approaches, the reporting component is responsible for determining the health of Falco and publishing the result as a Kubernetes node condition. NRC does not perform the health check itself. Instead, it watches the reported node condition and evaluates it using a `NodeReadinessRule` to manage readiness taints.

The two variants differ only in how the node condition is produced.

## `nrr-variant/`

Uses `readiness-condition-reporter` as a sidecar to publish a node condition representing Falco readiness.

The reporter checks Falco's HTTP health endpoint (`localhost:8765/healthz`) and publishes the result as `falco.org/FalcoReady`. The `NodeReadinessRule` removes the startup taint when `falco.org/FalcoReady` is `True`.

This approach is well suited when the component exposes an HTTP health endpoint that can be checked from within the same pod.

## `npd-variant/`

Uses Node Problem Detector (NPD) with a custom plugin that evaluates Falco health and publishes a node condition.

The NPD plugin checks whether Falco's port (`8765`) is reachable on the node and publishes the result as `falco.org/FalcoNotReady`. This is a problem-oriented condition, following the same convention as built-in Kubernetes node conditions such as `MemoryPressure` and `DiskPressure`, where `True` means a problem exists and `False` means the component is healthy. The `NodeReadinessRule` removes the startup taint when `falco.org/FalcoNotReady` is `False`.

This approach is well suited when health is best determined by running checks directly on the node, such as inspecting local ports, processes, or files, without requiring an HTTP endpoint.

## Shared files

| File | Description |
|---|---|
| `kind-cluster-config.yaml` | Kind cluster configuration for local testing. Worker nodes join with the `readiness.k8s.io/security-agent-ready=pending:NoSchedule` startup taint pre-applied. |
| `setup-falco.sh` | Script to install Falco on the cluster |
| `add-falco-toleration.sh` | Script to add a toleration to Falco so it can run on tainted nodes |