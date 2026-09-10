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

Duration of rule evaluations per rule.

| Property | Value |
| --- | --- |
| Type | `histogram` |
| Labels | `rule` |
| Buckets | Prometheus default histogram buckets |
| Recorded when | The controller evaluates a rule against a node |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `rule` | `NodeReadinessRule` name | Any rule name |

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

### `node_readiness_build_info`

*Available starting from the v0.6.0 release.*

Build information for the node-readiness-controller binary.

| Property | Value |
| --- | --- |
| Type | `gauge` |
| Labels | `version` |
| Recorded when | The controller starts up |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `version` | Build version of the running binary | Any version string, or `unknown` if not set at build time |

### `node_readiness_rule_nodes`

*Available starting from the v0.6.0 release.*

Number of nodes currently held or released by each `NodeReadinessRule`, collected at scrape time from the controller cache.

| Property | Value |
| --- | --- |
| Type | `gauge` |
| Labels | `rule`, `state` |
| Recorded when | Computed on each Prometheus scrape from the cached node list |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `rule` | `NodeReadinessRule` name | Any non-dry-run rule name with a valid selector |
| `state` | Whether matching nodes are still tainted by the rule or have had the taint removed | `held`, `released` |

### `node_readiness_blocked_nodes`

*Available starting from the v0.6.0 release.*

Number of currently-held nodes against blocking conditions per `NodeReadinessRule`.

| Property | Value |
| --- | --- |
| Type | `gauge` |
| Labels | `rule`, `condition` |
| Recorded when | Computed on each Prometheus scrape from the cached node list |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `rule` | `NodeReadinessRule` name | Any non-dry-run rule name |
| `condition` | Condition type declared in `spec.conditions` | Any condition type declared by the rule |

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

### `node_readiness_bootstrap_duration_seconds`

Time from node creation to bootstrap completion (taint removal) for bootstrap-only rules.

| Property | Value |
| --- | --- |
| Type | `histogram` |
| Labels | `rule` |
| Buckets | `1`, `5`, `10`, `30`, `60`, `120`, `300`, `600`, `1200` seconds |
| Recorded when | A bootstrap-only rule removes its taint from a node that was created after the rule. Nodes older than the rule are skipped so that they do not skew the histogram |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `rule` | `NodeReadinessRule` name | Any bootstrap-only rule name |

### `node_readiness_reconciliation_latency_seconds`

End-to-end latency from node condition change to taint operation completion.

| Property | Value |
| --- | --- |
| Type | `histogram` |
| Labels | `rule`, `operation` |
| Buckets | `0.01`, `0.05`, `0.1`, `0.5`, `1`, `2`, `5`, `10`, `30`, `60`, `120`, `300` seconds |
| Recorded when | The controller adds or removes a taint. The latency is measured from the most recent `lastTransitionTime` among the rule's conditions on that node, and clamped to zero if the node's clock is ahead of the controller's |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `rule` | `NodeReadinessRule` name | Any rule name |
| `operation` | Taint operation that completed the reconciliation | `add_taint`, `remove_taint` |

### `node_readiness_nodes_by_state`

Number of nodes in each readiness state per rule.

| Property | Value |
| --- | --- |
| Type | `gauge` |
| Labels | `rule`, `state` |
| Recorded when | The controller reconciles a rule or a node, and only when the controller runs with `--enable-node-state-metrics`. The counts come from the rule's `status.nodeEvaluations` |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `rule` | `NodeReadinessRule` name | Any rule name |
| `state` | Readiness state derived from the taint status of the node under the rule | `ready` (taint absent), `not_ready` (taint present under a continuous rule), `bootstrapping` (taint present under a bootstrap-only rule) |

### `node_readiness_condition_failures_total`

Total number of failed condition evaluations by rule and condition name.

| Property | Value |
| --- | --- |
| Type | `counter` |
| Labels | `rule`, `condition` |
| Recorded when | A condition listed in `spec.conditions` does not have its required status on a node during evaluation |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `rule` | `NodeReadinessRule` name | Any rule name |
| `condition` | Condition type declared in `spec.conditions` | Any condition type declared by the rule |

### `node_readiness_rule_last_reconciliation_timestamp_seconds`

Unix timestamp of the last rule reconciliation.

| Property | Value |
| --- | --- |
| Type | `gauge` |
| Labels | `rule` |
| Recorded when | The rule controller finishes cleaning up deleted nodes for a rule, on every reconciliation of that rule |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `rule` | `NodeReadinessRule` name | Any rule name |

## Reporter Metrics

The `readiness-condition-reporter` serves its own Prometheus metrics on `/metrics`, on the address configured by `METRICS_BIND_ADDRESS`. See [Reporter Configuration](../reference/reporter-configuration.md) for deployment details.

### `node_readiness_reporter_build_info`

Reporter binary version to track fleet version skew.

| Property | Value |
| --- | --- |
| Type | `gauge` |
| Labels | `version` |
| Recorded when | The reporter process starts |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `version` | Reporter binary version | Any version string |

### `node_readiness_reporter_check_duration_seconds`

Duration of health probe checks.

| Property | Value |
| --- | --- |
| Type | `histogram` |
| Labels | none |
| Buckets | `0.005, 0.1, 0.25, 0.5, 1, 2.5, 5, 10` seconds |
| Recorded when | The reporter completes a health check request to `CHECK_ENDPOINT` |

### `node_readiness_reporter_checks_total`

Total probe check results over time.

| Property | Value |
| --- | --- |
| Type | `counter` |
| Labels | `result` |
| Recorded when | The reporter completes a health check and classifies the result |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `result` | Outcome of the health check | `healthy`, `unhealthy`, `error` |

### `node_readiness_reporter_condition_writes_total`

Total node condition update outcomes, including writes skipped when state was unchanged.

| Property | Value |
| --- | --- |
| Type | `counter` |
| Labels | `result` |
| Recorded when | The reporter evaluates whether to write the Node condition after a health check, including when the write is skipped |

#### Labels

| Label | Description | Values |
| --- | --- | --- |
| `result` | Outcome of the condition write attempt | `success`, `error`, `skipped` |
