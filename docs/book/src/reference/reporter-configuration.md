# Reporter Configuration

The `readiness-condition-reporter` is one of the ways Node Readiness Controller
supports reporting node conditions from the node itself.

This page lists configuration variables for deploying the
`readiness-condition-reporter`. These can be configured through environment
variables on its container. See the [CNI Readiness](../examples/cni-readiness.md)
and [Security Agent](../examples/security-agent-readiness.md) for examples.

## Environment Variables

| Variable | Description | Default |
| --- | --- | --- |
| `NODE_NAME` | The name of the Node this reporter instance updates. Typically set via the Downward API (`fieldRef: spec.nodeName`) rather than a literal value. | (required) |
| `CHECK_ENDPOINT` | The local HTTP endpoint the reporter polls to determine health, e.g. `http://localhost:9099/readiness`. | (required) |
| `CONDITION_TYPE` | The Node Readiness condition written by the reporter, e.g. `projectcalico.org/CalicoReady`. | (required) |
| `CHECK_INTERVAL` | How often the reporter polls `CHECK_ENDPOINT`. | `30s` |
| `HEARTBEAT_PERIOD` | The maximum time the reporter can go without writing to the Node's condition if component health hasn't changed. See [Optimizing node status writes](../user-guide/concepts.md#optimizing-node-status-writes). Accepts Go duration strings like `30s`, `2m`, `1h`. Invalid values are logged and fall back to the default. | `5m` |
| `IMPERSONATE_NODE` | When set to `"true"`, the reporter sends `Impersonate-User: system:node:<nodeName>` headers on every request, enabling the constrained impersonation authorization flow. Requires Kubernetes **v1.35+** for [KEP-5284](https://github.com/kubernetes/enhancements/issues/5284) (Constrained Impersonation) to actually be enforced by the API server. See [Security](../operations/security.md#reporter-configuration) for details. | unset (uses the reporter's own ServiceAccount identity) |

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
