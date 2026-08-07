# Observability Design Document: Node Readiness Controller

### LFX Project: Granular Metrics and Improving SLIs from NRC

This project brings better visibility into the Node Readiness Controller.

The project answers two main questions:

1. **Readiness as an SLO across owners:** Different teams own different readiness rules across the stack. Networking owns CNI readiness, storage owns CSI, and a hardware team owns GPU.
When a node takes too long to become available, operators need to see which stage was on the critical path and how long it took, both on average and at the tail. They need to know whether the node was held by the built-in `Ready` condition from kubelet or by a specific readiness rule. The goal is to attribute delays to a stage and owner without reading controller logs, node logs, or raw YAML.

2. **Fleet-wide availability:** Across the fleet, operators need to know which nodes or pools are not available for a given requirement. For example, they need to see what percentage of nodes are not GPU-ready. This is a roll-up over nodes, sliced by rule.

### Goals

* Add granular per-rule metrics.
* Build a Grafana dashboard for fleet-wide SLOs.

### Non-Goals

* Build a Headlamp plugin. This was originally a goal, but we rescoped it as a follow-up project for LFX Term 3.

## Design

### 1. Personas

We design observability around three distinct personas. Each persona asks different questions and looks at different interfaces.

1. **Infrastructure Owners (Cluster Operators)**: These operators run the cluster and manage the node lifecycle. They need to know if the fleet is healthy and if the controller is working.
2. **Component / Rule Owners**: These are teams that own the infrastructure components that gate node readiness, like CNI plugins or GPU drivers. They need to know whether / which specific check is failing.
3. **Workload Owners (Application developers)**: These users run application pods on the cluster. They care about scheduling delays and node availability.

---

### 2. Signals

We identify four telemetry surface in NRC: metric, status, event, or log and will follow below principles to assign each observability need to the right channel.

#### Metric vs status vs event vs log decisions
* Metrics to track aggregate cluster rates, durations, and counts over time. NRC will never use node names as metric labels for scale concerns.
* Custom Resource status (`NodeReadinessRule.status`) will hold current rule health and evaluation summaries. New `NodeReadinessEvaluation` api will also be implemented for the individual node readiness status.
* Kubernetes events will record single-node lifecycle actions like `TaintAdded`, `TaintRemoved`, `TaintAdopted`, and `BootstrapCompleted`.
* Logs capture detailed controller traces. In managed Kubernetes environments, customers cannot read controller logs, so metrics and status must answer the operational questions for other personas.

#### Core signals
| Signal | Instrument | Decision |
|---|---|---|
| **Rule discovery (count, mode)** | Metric (`node_readiness_rules`) + `kubectl get nrr` | While `kubectl get` provides an API-level list, this gauge gives operators a controller-side view. The existing `node_readiness_rules_total` gauge violates naming conventions and ignores rule modes. We deprecate it in favor of `node_readiness_rules` without the `_total` suffix, adding labels to slice by `enforcement_mode` and `dry_run`. |
| **Selector match (nodes grouped per rule)** | Metric (`node_readiness_rule_matched_nodes`) | Tracks how many nodes match each rule selector. Because we are removing per-node lists from `NodeReadinessRule.status` for scalability, this scrape-time gauge collector preserves visibility into selector reach. A zero value signals a misconfigured selector. |
| **Nodes held or released per rule** | Metric (`node_readiness_rule_nodes`) | Shows the direct capacity impact of readiness gating on the cluster. The existing `node_readiness_nodes_by_state` gauge requires scale testing and tuning. We replace it with an always-on scrape-time collector that reads ground-truth `spec.taints` directly from the node cache. |
| **Blocking conditions per rule** | Metric (`node_readiness_blocked_nodes`) | Assesses which infrastructure component is blocking nodes across the fleet. Today, `node_readiness_condition_failures_total` counts an unsatisfied condition during normal bootstrapping as a failure, which inflates rates with every reconcile. We replace it with a `node_readiness_blocked_nodes{rule,condition}` gauge. Per-node drill-downs will live in the upcoming `NodeReadinessEvaluation` CRD. |
| **Per-node evaluation outcome** | CR status (`NodeReadinessEvaluationStatus`) + Node Events | Detailed per-node evaluation state moves to the new `NodeReadinessEvaluation` CRD. In addition, `markBootstrapCompleted` will emit a Kubernetes Node Event similar to taint operations so operators can inspect bootstrap completion in `kubectl describe node`. |
| **Per-node readiness condition state** | `kube_node_status_condition` (kube-state-metrics) + Doc update | No controller code changes needed. `kube-state-metrics` already exports custom node conditions. Because we do not enforce a fixed prefix like `readiness.k8s.io` for conditions, we will update our documentation to recommend domain-scoped condition types (such as `network.example.io/CustomConditionReady`) so teams can group and monitor them in Grafana. |
| **Taint add and remove activity** | Counter (`node_readiness_taint_operations_total`) + Node Events | Retain both channels. The counter tracks rate, churn, and flapping across the fleet, while Kubernetes Events tell the lifecycle story on individual nodes. |
| **Operational failures** | Counter (`node_readiness_failures_total`) | Consolidates all operational errors across evaluations, taint operations, and status patches into a single counter. It uses a fixed vocabulary of snake_case reason strings for clear alerting. |
| **Rule staleness and aliveness** | CR status conditions + workqueue depth | Remove `node_readiness_rule_last_reconciliation_timestamp_seconds`. Under `GenerationChangedPredicate` without resyncs, an unchanged rule is still actively enforced, making old timestamps false alarms. We track controller activity using evaluation rate and workqueue depth. |
| **Bootstrap hold duration** | Histogram (`node_readiness_bootstrap_hold_duration_seconds`) | Measures total unschedulable time caused by readiness gating. A `taint_origin` label separates taints applied by the controller (`controller`) from those adopted at boot (`adopted`). |

---

### 3. Proposed Metric Surface

The target metric surface freezes at `v1beta1`. Controller metrics use the `node_readiness_` prefix, and reporter metrics use the `node_readiness_reporter_` prefix.

#### Controller metrics

| Metric name | Type | Labels | Description | Stability tier |
|---|---|---|---|---|
| `node_readiness_build_info` | gauge | `version` | Controller binary version for managed-export monitoring. | ALPHA |
| `node_readiness_rules` | gauge | `enforcement_mode`, `dry_run` | Number of rules by enforcement mode and dry-run state. | BETA |
| `node_readiness_rule_matched_nodes` | gauge | `rule` | Number of nodes matching the rule selector. | BETA |
| `node_readiness_rule_nodes` | gauge | `rule`, `state` (`held` or `released`) | Number of nodes currently gated or released by the rule. | BETA |
| `node_readiness_blocked_nodes` | gauge | `rule`, `condition` | Number of nodes blocked by each required condition. | ALPHA |
| `node_readiness_taint_operations_total` | counter | `rule`, `operation` (`add` or `remove`) | Total taint additions and removals performed by the controller. | BETA (STABLE candidate) |
| `node_readiness_failures_total` | counter | `rule`, `reason` | Total operational failures across evaluation, taint operations, and status patches. | BETA |
| `node_readiness_api_conflicts_total` | counter | `rule`, `operation` | Number of API write conflicts per retry attempt. | ALPHA |
| `node_readiness_evaluation_duration_seconds` | histogram | `rule` | Duration of rule evaluation and enforcement per node. | ALPHA |
| `node_readiness_enforcement_latency_seconds` | histogram | `rule`, `operation` (`add` or `remove`) | Latency from condition transition to taint addition or removal. | BETA |
| `node_readiness_bootstrap_hold_duration_seconds` | histogram | `rule`, `taint_origin` (`controller` or `adopted`) | Total duration a node remained unschedulable due to readiness gating. | BETA |
| `node_readiness_bootstrap_completed_total` | counter | `rule` | Number of nodes that completed bootstrap taint removal. | BETA |

#### Reporter metrics (new proposed for daemonset)
We expose these metrics on `/metrics` and `/healthz` for each reporter pod.

| Metric name | Type | Labels | Description | Stability tier |
|---|---|---|---|---|
| `node_readiness_reporter_build_info` | gauge | `version` | Reporter binary version to track fleet version skew. | ALPHA |
| `node_readiness_reporter_check_duration_seconds` | histogram | none | Duration of health probe checks. | ALPHA |
| `node_readiness_reporter_checks_total` | counter | `result` (`healthy`, `unhealthy`, or `error`) | Total probe check results over time. | ALPHA |
| `node_readiness_reporter_condition_writes_total` | counter | `result` (`success`, `error`, or `skipped`) | Total condition write attempts to the API server. | ALPHA |

Note 1: Some metrics are intentionally started as BETA as they feed to alerting rules and SLO dashboards. Marking them BETA protects from sudden breaking changes.
Note 2: Additional metrics for Node-Readiness-Daemon (NRD) will be followed-up as a separate design update to this document.

#### SLIs / SLOs

Below table captures how NRC can be used to build alerts for above personas.

| Persona | Question | SLI / SLO | Alerts and health checks |
|---|---|---|---|
| NRC cluster operators | Are there nodes stuck in a bootstrapping state? | **Nodes held:** `sum by (rule) (node_readiness_rule_nodes{state="held"})` | **Node Capacity held:** `sum(node_readiness_rule_nodes{state="held"}) > 0` and zero bootstrap completions for 30m.<br><br>**Error budget:** `rate(node_readiness_failures_total[10m]) > 0` for 15m.<br><br>**Controller health:** `up == 0`, `workqueue_depth > 1000`. |
| Component and rule owners | Is my component blocking node readiness across the cluster? | **Blocking condition:** `max by (rule, condition) (node_readiness_blocked_nodes) > threshold` for 30m | **Rule errors:** `rate(node_readiness_failures_total{rule="my-rule"}[10m]) > 0` for 15m.<br><br>**Reporter health:** Aggregate error rate on condition writes. |
| Workload owners | How long does readiness gating delay pod scheduling? | **Bootstrap hold p99:** `histogram_quantile(0.99, ...)` across controller and adopted taint origins.<br><br>**Release latency SLO:** `< 60s` from condition ready to taint removal. | **Flapping check:** Monitor `rate(node_readiness_taint_operations_total[30m])` for continuous add and remove cycling. |


#### Other Documented Metrics (not-owned by NRC)
The monitoring guide will also document below built-in metrics from `controller-runtime` and `kube-state-metrics` relevant to NRC:
* `up` and `leader_election_master_status`
* `workqueue_depth` and `workqueue_queue_duration_seconds`
* `controller_runtime_reconcile_errors_total` and `rest_client_requests_total`
* `kube_node_status_condition` from `kube-state-metrics` for per-node condition visibility.

#### Failure reasons
The `node_readiness_failures_total` counter uses a fixed set of snake_case reason strings:
* `evaluation_error`
* `add_taint_error` and `add_taint_conflict_exhausted`
* `remove_taint_error` and `remove_taint_conflict_exhausted`
* `status_patch_error` and `status_patch_conflict_exhausted`
* `bootstrap_completion_mark_error`

---

### 4. New Scrape-Time Collector Surface

We implement a custom scrape-time collector to serve cluster-wide / high-cardinality metrics. This replaces the implementation of standard event-driven gauges (`prometheus.NewGaugeVec`) that are updated inside controller reconciliation loops.

#### Limitations of event-driven gauges
Today, reconcilers call `.Set()` or `.Inc()` during their event loops (for example, inside `SyncNodeStateMetrics` in `node_controller.go`). This pattern has some drawbacks:
* Reconcilers spend CPU cycles and take mutex locks to update gauge math rather than focusing purely on node readiness gating. This adds delay in handling the nodes.
* If a rule stops reconciling or an error occurs early in the loop, event-driven gauges freeze and report stale numbers.
* When an operator deletes a `NodeReadinessRule` or `Node`, reconcilers must manually call `DeletePartialMatch` to clean up old label combinations in the Prometheus registry. This could possibly leak memory over time.

#### Proposed: On-demand collection from the Informer cache
We implement the `prometheus.Collector` interface (`Describe` and `Collect` methods) in a new `internal/metrics/collector.go` package, following the [Prometheus Go Client Custom Collectors](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus#Collector) documentation.

When Prometheus scrapes the `/metrics` endpoint, the HTTP server calls `Collect(ch chan<- prometheus.Metric)` on demand:
1. The collector holds a read-only reference to the controller's Informer cache (`client.Reader`).
2. Inside `Collect()`, it lists cached `Node` and `NodeReadinessRule` objects directly from local memory.
3. It computes counts on the fly and yields immutable gauge values (`prometheus.MustNewConstMetric`) directly to the scrape channel.

#### Metrics served by the custom collector
The scrape-time collector serves below fleet-wide signals:
* **`node_readiness_rule_nodes{rule, state="held"|"released"}`:** Reads ground-truth `spec.taints` and status conditions from cached nodes to report cluster capacity impact.
* **`node_readiness_rule_matched_nodes{rule}`:** Counts how many cached nodes match each rule's label selector, exposing misconfigured selectors (`0` matches).
* **`node_readiness_blocked_nodes{rule, condition}`:** Inspects unsatisfied condition-requirements across bootstrapping nodes to capture which infrastructure component is blocking readiness.

This benefits the Gauges to always reflect the Informer cache at the exact second Prometheus scrapes the endpoint and helps with reconcile overhead in scale.

---

### 5. Decisions and Rollout Plan

#### Evaluation of existing metrics

| Existing metric | Decision | Target metric and migration plan |
|---|---|---|
| `node_readiness_taint_operations_total` | Keep | Retain as-is (`rule`, `operation`). |
| `node_readiness_bootstrap_completed_total` | Keep | Retain as-is (`rule`). |
| `node_readiness_evaluation_duration_seconds` | Reshape | Keep `rule` label; update help string to clarify it includes taint API calls. |
| `node_readiness_failures_total` | Reshape | Migrate reason values to standard snake_case strings (refer Failure reasons). |
| `node_readiness_rules_total` | Rename and reshape | Migrate to `node_readiness_rules{enforcement_mode, dry_run}` gauge. Drop `_total` suffix from gauge. |
| `node_readiness_nodes_by_state` | Rename and reshape | Replace status-derived gauge with an always-on scrape-time collector `node_readiness_rule_nodes{rule, state="held"|"released"}`. The metrics need to be scale-tested and flag enabled by default |
| `node_readiness_bootstrap_duration_seconds` | Rename and reshape | Migrate into `node_readiness_bootstrap_hold_duration_seconds{rule, taint_origin="adopted"}` histogram. |
| `node_readiness_reconciliation_latency_seconds` | Rename and reshape | Rename to `node_readiness_enforcement_latency_seconds` to prevent collision with `controller-runtime` reconcile metrics. Normalize operation values to `add` and `remove`. |
| `node_readiness_condition_failures_total` | Delete | Replace state-as-counter metric with the `node_readiness_blocked_nodes` gauge. |
| `node_readiness_rule_last_reconciliation_timestamp_seconds` | Delete | Remove timestamp gauge. Unchanged rules are still actively enforced under `GenerationChangedPredicate` without resyncs. |

We apply all metric renames, label changes, and deletions in one breaking release and document them. We deprecate and dual-publish old and new metric shapes during alpha, and drop the old shapes when graduating to beta.

#### Stability tiers for future
We define three stability tiers in the documentation following Kubernetes [instrumentation guidelines](https://kubernetes.io/docs/reference/instrumentation/metrics/):
* **ALPHA:** The metric may change or be removed in any minor release.
* **BETA:** The metric is a freeze candidate for `v1beta1`. Renaming or changing labels requires a formal deprecation period.
* **STABLE:** The metric name, type, and labels are frozen.

When a BETA metric is deprecated after freeze, we dual-publish it for one minor release with a `[DEPRECATED since vX.Y]` prefix in the help text before removing it.

**Graduation criteria:** A metric graduates from Alpha to Beta after running in production across at least one minor release with stable label cardinality and proven use in alerts or dashboards. A Beta metric graduates to Stable once the underlying API reaches `v1` (or `v1beta1` freeze) and the metric shape has remained unchanged across two or more minor releases. Ref: [KEP-1206](https://github.com/kubernetes/enhancements/tree/master/keps/sig-instrumentation/1206-metrics-overhaul), [KEP-1209](https://github.com/kubernetes/enhancements/tree/master/keps/sig-instrumentation/1209-metrics-stability), [KEP-3498](https://github.com/kubernetes/enhancements/tree/master/keps/sig-instrumentation/3498-extending-stability)

#### Guardrails
We enforce metric standards mechanically in CI:
* A unit test using `testutil.CollectAndLint` checks every registered metric for violations, eg., `_total` suffixes on gauges.
* A documentation test verifies that every registered metric appears in `docs/book/src/operations/monitoring.md`.
* Pull requests cannot add new metrics without declaring a stability tier and adding documentation.
