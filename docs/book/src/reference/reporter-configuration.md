# Reporter Configuration

The `readiness-condition-reporter` is one of the ways Node Readiness Controller
supports reporting node conditions from the node.

This page lists configuration variables for deploying the
`readiness-condition-reporter`. These can be configured through environment
variables on its container. See the [CNI Readiness](../examples/cni-readiness.md)
and [Security Agent](../examples/security-agent-readiness.md) for examples.

## Environment Variables

| Variable | Description | Default |
| --- | --- | --- |
| `NODE_NAME` | The host name of the underlying Node. The reporter uses it to identify the Node object in the API server for the readiness updates. Typically set via the Downward API (`fieldRef: spec.nodeName`). | (required) |
| `CHECK_ENDPOINT` | The HTTP endpoint the reporter polls to determine component health, e.g. `http://localhost:9099/healthz`. | (required) |
| `CONDITION_TYPE` | The Node Readiness condition written by the reporter, e.g. `projectcalico.org/CalicoReady`. | (required) |
| `CHECK_INTERVAL` | How often the reporter polls `CHECK_ENDPOINT`. | `30s` |
| `HEARTBEAT_PERIOD` | The maximum time the reporter can go without writing to the Node's condition if component health hasn't changed. See [Optimizing node status writes](../user-guide/concepts.md#optimizing-node-status-writes). Accepts Go duration strings like `30s`, `2m`, `1h`. Invalid values are logged and fall back to the default. | `5m` |
| `IMPERSONATE_NODE` | When set to `"true"`, the reporter sends `Impersonate-User: system:node:<nodeName>` headers on every request, enabling the constrained impersonation authorization flow. Requires Kubernetes **v1.35+** for [Constrained Impersonation](https://kubernetes.io/docs/reference/access-authn-authz/user-impersonation/#constrained-impersonation) feature. See [Security](../operations/security.md#reporter-configuration) for details. | unset (uses the reporter's own ServiceAccount identity) |
| `METRICS_BIND_ADDRESS` | The bind address for the reporter's `/metrics` (Prometheus) and `/healthz` HTTP endpoints. | `:9445` |

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
