# Monitoring

Node Readiness Controller exposes Prometheus-compatible metrics. This page describes the Prometheus metrics exposed by Node Readiness Controller for monitoring rule evaluation, taint operations, failures, and bootstrap progress.

## Metrics Endpoint

The controller serves metrics on `/metrics` only when metrics are explicitly enabled. Depending on the installation, the endpoint is served either over HTTP or over HTTPS. See [Installation](../user-guide/installation.md) for deployment details.

## Supported Metrics

### `node_readiness_rules_total`

Number of `NodeReadinessRule` objects tracked by the controller.

| Property | Value |
| --- | --- |
| Type | `gauge` |
| Labels | none |
| Recorded when | The controller refreshes or removes a tracked rule |

### `node_readiness_taint_operations_total`

Total number of taint operations performed by the controller.

| Property | Value |
| --- | --- |
| Type | `counter` |
| Labels | `rule`, `operation` |
| Recorded when | The controller successfully adds or removes a taint |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `rule` | `NodeReadinessRule` name | Any rule name |
| `operation` | Taint operation performed by the controller | `add`, `remove` |

### `node_readiness_evaluation_duration_seconds`

Duration of rule evaluations.

| Property | Value |
| --- | --- |
| Type | `histogram` |
| Labels | none |
| Buckets | Prometheus default histogram buckets |
| Recorded when | The controller evaluates a rule against a node |

### `node_readiness_failures_total`

Total number of failure events recorded by the controller.

| Property | Value |
| --- | --- |
| Type | `counter` |
| Labels | `rule`, `reason` |
| Recorded when | The controller records an evaluation failure or taint add/remove failure |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `rule` | `NodeReadinessRule` name | Any rule name |
| `reason` | Failure label recorded by the controller | `EvaluationError`, `AddTaintError`, `RemoveTaintError` |

### `node_readiness_bootstrap_completed_total`

Total number of nodes that have completed bootstrap.

| Property | Value |
| --- | --- |
| Type | `counter` |
| Labels | `rule` |
| Recorded when | The controller marks bootstrap as completed for a node under a bootstrap-only rule |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `rule` | `NodeReadinessRule` name | Any rule name |
