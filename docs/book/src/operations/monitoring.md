# Monitoring

The Node Readiness Controller exposes Prometheus-compatible metrics. This page documents the metrics currently registered by the controller and how they can be used for monitoring rule evaluation, taint operations, failures, bootstrap progress, and rule health.

## Metrics Endpoint

The controller serves metrics on `/metrics` only when metrics are explicitly enabled.

Depending on the installation, the endpoint is exposed as:

  - HTTP on port `8080` when the standard Prometheus component is enabled.
  - HTTPS on port `8443` when the Prometheus TLS component is enabled.

See [Installation](https://www.google.com/search?q=../user-guide/installation.md) for deployment details.

## Metric Lifecycle Management

When a `NodeReadinessRule` is deleted, the controller automatically cleans up the associated rule-labeled Prometheus series. This prevents stale metrics from remaining visible in dashboards and alerts.

**Metrics cleaned up on rule deletion:**

  - `node_readiness_taint_operations_total{rule="..."}`
  - `node_readiness_evaluation_duration_seconds{rule="..."}`
  - `node_readiness_failures_total{rule="..."}`
  - `node_readiness_bootstrap_completed_total{rule="..."}`
  - `node_readiness_reconciliation_latency_seconds{rule="..."}`
  - `node_readiness_bootstrap_duration_seconds{rule="..."}`
  - `node_readiness_nodes_by_state{rule="..."}`
  - `node_readiness_rule_last_reconciliation_timestamp_seconds{rule="..."}`

This ensures that:

  - Deleted rules do not continue to appear in dashboards with stale values.
  - Memory usage does not grow unbounded from removed rules.
  - Metric cardinality remains highly accurate over time.

**Note:** The global `node_readiness_rules_total` gauge is updated separately. Rule-labeled metrics are explicitly deleted during rule cleanup.

-----

## Core Metrics

### `node_readiness_rules_total`

Number of `NodeReadinessRule` objects currently tracked by the controller.

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
| Labels | `rule`, `operation` (`add`, `remove`) |
| Recorded when | The controller successfully adds or removes a taint |

### `node_readiness_evaluation_duration_seconds`

Duration of the controller's internal rule evaluations.

| Property | Value |
| --- | --- |
| Type | `histogram` |
| Labels | `rule` |
| Buckets | Prometheus default histogram buckets |
| Recorded when | The controller evaluates a rule against a node |

### `node_readiness_failures_total`

Total number of failure events recorded by the controller.

| Property | Value |
| --- | --- |
| Type | `counter` |
| Labels | `rule`, `reason` (`EvaluationError`, `AddTaintError`, `RemoveTaintError`) |
| Recorded when | The controller encounters an error evaluating or patching a node |

### `node_readiness_bootstrap_completed_total`

Total number of nodes that have completed bootstrap.

| Property | Value |
| --- | --- |
| Type | `counter` |
| Labels | `rule` |
| Recorded when | The controller marks bootstrap as completed for a node under a bootstrap-only rule |

-----

## Extended Health and SLI Metrics

### `node_readiness_reconciliation_latency_seconds`

End-to-end latency from node condition change to taint operation completion.

| Property | Value |
| --- | --- |
| Type | `histogram` |
| Labels | `rule`, `operation` (`add_taint`, `remove_taint`) |
| Buckets | `0.01`, `0.05`, `0.1`, `0.5`, `1`, `2`, `5`, `10`, `30`, `60`, `120`, `300` seconds |
| Recorded when | A taint operation completes |

**Use case:** Measure how quickly the controller responds to node condition changes in the cluster.

### `node_readiness_bootstrap_duration_seconds`

Time from node creation to bootstrap completion for bootstrap-only rules.

| Property | Value |
| --- | --- |
| Type | `histogram` |
| Labels | `rule` |
| Buckets | `1`, `5`, `10`, `30`, `60`, `120`, `300`, `600`, `1200` seconds |
| Recorded when | Bootstrap completion is observed for a node under a bootstrap-only rule |

**Use case:** Measure the actual time nodes take to become fully provisioned and bootstrap-complete.

### `node_readiness_nodes_by_state`

Number of nodes in each readiness state per rule.

| Property | Value |
| --- | --- |
| Type | `gauge` |
| Labels | `rule`, `state` (`ready`, `not_ready`, `bootstrapping`) |
| Recorded when | A rule reconciliation completes |

**Use case:** Track aggregate node health without introducing per-node metric cardinality, keeping controller memory footprint lean.

### `node_readiness_rule_last_reconciliation_timestamp_seconds`

Unix timestamp of the last reconciliation for a rule.

| Property | Value |
| --- | --- |
| Type | `gauge` |
| Labels | `rule` |
| Recorded when | A rule reconciliation loop successfully completes |

**Use case:** Detect rules that may be stuck or not reconciling frequently enough.

-----

## Example Queries & SLOs

### Latency Monitoring & SLOs

**Objective:** 95% of internal evaluations complete within 50 milliseconds (0.05s).

```promql
# Percentage of evaluations completing within 50ms
sum(rate(node_readiness_evaluation_duration_seconds_bucket{le="0.05"}[5m])) /
sum(rate(node_readiness_evaluation_duration_seconds_count[5m])) * 100
```

```promql
# P95 End-to-End Reconciliation Latency across all rules
histogram_quantile(0.95,
  sum by (le) (
    rate(node_readiness_reconciliation_latency_seconds_bucket[5m])
  )
)
```

### Freshness Monitoring & SLOs

**Objective:** All rules reconcile within the last 2 minutes.

```promql
# Alert if any rule has not reconciled in the last 120 seconds
(time() - node_readiness_rule_last_reconciliation_timestamp_seconds) > 120
```

### Availability Monitoring & SLOs

**Objective:** 99.9% of targeted nodes are ready.

```promql
# Percentage of ready nodes globally
100 * sum(node_readiness_nodes_by_state{state="ready"}) / sum(node_readiness_nodes_by_state)

# Percentage of ready nodes per rule
100 * node_readiness_nodes_by_state{state="ready"} / sum by (rule) (node_readiness_nodes_by_state)
```

## Monitoring and Scale Testing

For an end-to-end monitoring setup with Prometheus and Grafana during scale tests, see the [scale testing guide](../../../../hack/test-workloads/scale/README.md).

## Alerting Recommendations

Typical alerts to consider:

  - **High latency:** P95 reconciliation latency above 10s for 5 minutes.
  - **Stale reconciliations:** Any rule with no reconciliation for more than 5 minutes.
  - **High failure rate:** Sustained increase in `node_readiness_failures_total`.
  - **Low availability:** Ready-node percentage below your target threshold for a sustained period.