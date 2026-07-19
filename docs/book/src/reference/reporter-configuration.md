# Reporter Configuration

The `readiness-condition-reporter` is configured entirely through environment
variables on its container. This page lists all supported variables. For a
walkthrough of deploying the reporter, see the [CNI Readiness](../examples/cni-readiness.md)
and [Security Agent](../examples/security-agent-readiness.md) examples.

## Environment Variables

| Variable | Description | Default |
| --- | --- | --- |
| `NODE_NAME` | The name of the Node this reporter instance updates. Typically set via the Downward API (`fieldRef: spec.nodeName`) rather than a literal value. | (required) |
| `CHECK_ENDPOINT` | The local HTTP endpoint the reporter polls to determine health, e.g. `http://localhost:9099/readiness`. | (required) |
| `CONDITION_TYPE` | The Node condition type the reporter writes, e.g. `projectcalico.org/CalicoReady`. | (required) |
| `CHECK_INTERVAL` | How often the reporter polls `CHECK_ENDPOINT`. | `30s` |
| `HEARTBEAT_PERIOD` | The maximum time the reporter can go without writing to the Node's condition, even when the observed health status hasn't changed. See [Reducing unnecessary node status writes](../user-guide/concepts.md#reducing-unnecessary-node-status-writes) for details. Accepts a Go duration string (`30s`, `2m`, `1h`). Invalid values are logged and fall back to the default. | `5m` |
| `IMPERSONATE_NODE` | When set to `"true"`, the reporter sends `Impersonate-User: system:node:<nodeName>` headers on every request, enabling the constrained impersonation authorization flow. See [Security](../operations/security.md#reporter-configuration) for details. | unset (uses the reporter's own ServiceAccount identity) |

> [!NOTE]
> `NODE_NAME`, `CHECK_ENDPOINT`, and `CONDITION_TYPE` together define *what*
> the reporter checks and *where* it writes the result. `CHECK_INTERVAL` and
> `HEARTBEAT_PERIOD` control *how often* it checks and *how often* it is
> guaranteed to write, respectively i.e. these are two different clocks and not
> the same setting.

## Example

```yaml
env:
  - name: NODE_NAME
    valueFrom:
      fieldRef:
        fieldPath: spec.nodeName
  - name: CHECK_ENDPOINT
    value: "http://localhost:9099/readiness"
  - name: CONDITION_TYPE
    value: "projectcalico.org/CalicoReady"
  - name: CHECK_INTERVAL
    value: "30s"
  - name: HEARTBEAT_PERIOD
    value: "5m"
```
