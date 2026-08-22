# Node Readiness Controller Examples

This directory contains examples demonstrating different ways to report node conditions that can be consumed by the Node Readiness Controller (NRC).

NRC does not perform health checks itself. Instead, it watches `node.status.conditions` on Kubernetes Node objects and evaluates those conditions against configured `NodeReadinessRule` resources. When a rule matches, NRC applies or removes the corresponding readiness taints.

The examples in this directory differ only in how the node condition is produced. In every case, the condition is published to `node.status.conditions`, and NRC reacts to the reported state without requiring any knowledge of the reporting component.

## Shared Architecture

Each example in this directory follows the same interaction model:

```text
Health Check
      │
      ▼
Condition Producer
      │
      ▼
node.status.conditions
      │
      ▼
Node Readiness Controller
      │
      ▼
Readiness Taints
```

The condition producer is responsible for determining the health of a node component and publishing the result as a node condition. The producer may be a dedicated reporting agent, the component itself, Node Problem Detector (NPD), or another implementation.

## Available Examples

The examples in this directory demonstrate different ways of producing node conditions for common node readiness scenarios. While the reporting mechanism varies, each example publishes a node condition that NRC evaluates through a `NodeReadinessRule`.

### `cni-readiness/`

Demonstrates using readiness-condition-reporter to monitor a CNI plugin (Calico) through its HTTP health endpoint and publish the result as a node condition.

Reporting mechanism: readiness-condition-reporter as a standalone DaemonSet

### `security-agent-readiness/`

Demonstrates two different implementations for reporting the readiness of a security agent:

- **`nrr-variant/`** uses `readiness-condition-reporter` as a sidecar to publish the condition via Falco's HTTP health endpoint.
- **`npd-variant/`** uses a Node Problem Detector (NPD) custom plugin to evaluate Falco health and publish the condition directly on the node.

These variants demonstrate that the reporting mechanism can change without requiring changes to NRC.

Reporting mechanisms: readiness-condition-reporter (sidecar), Node Problem Detector (NPD)

### `static-pod/`

Demonstrates deploying NRC itself as a static pod on control-plane nodes. This is suited for clusters where the controller needs to be available during early bootstrap, before the API server is fully operational, or where high availability of the controller across control-plane nodes is required.

### `constrained-impersonation/`

Demonstrates the same CNI readiness pattern as `cni-readiness/`, using the constrained node impersonation feature of `readiness-condition-reporter`. Each reporter pod impersonates its own node's identity when writing conditions, so that a compromised reporter can only affect the node it runs on.

## Reporting Mechanisms

Node conditions can be reported to NRC in several ways. The examples in this
directory demonstrate the following mechanisms:

| Mechanism | Description | Examples |
|---|---|---|
| `readiness-condition-reporter` (DaemonSet) | A standalone DaemonSet checks an HTTP endpoint and writes the condition | `cni-readiness/`, `constrained-impersonation/` |
| `readiness-condition-reporter` (sidecar) | The reporter runs as a sidecar inside the component's own pod | `security-agent-readiness/nrr-variant/` |
| Node Problem Detector (NPD) | An NPD custom plugin script runs on the node and writes the condition | `security-agent-readiness/npd-variant/` |