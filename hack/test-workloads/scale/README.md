# Node Readiness Controller - Scale Testing Guide

This guide explains how to run scale tests for Node Readiness Controller (NRR) with Prometheus and Grafana, and how to interpret the metrics that are currently emitted by the controller.

## Table of Contents

- [Overview](#overview)
- [Metrics Available During Scale Tests](#metrics-available-during-scale-tests)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Detailed Setup](#detailed-setup)
- [Import the Grafana Dashboard](#import-the-grafana-dashboard)
- [Run Scale Tests](#run-scale-tests)
- [Monitoring Queries](#monitoring-queries)
- [Interpreting Results](#interpreting-results)
- [Troubleshooting](#troubleshooting)
- [Cleanup](#cleanup)
- [Advanced Usage](#advanced-usage)
- [Additional Resources](#additional-resources)

## Overview

The scale test framework allows you to:

  - Test NRR with multiple nodes using [KWOK](https://kwok.sigs.k8s.io/) (fake nodes).
  - Measure taint addition and removal throughput.
  - Observe evaluation logic performance in Grafana.
  - Monitor controller resource usage (memory and CPU).
  - Inspect `controller-runtime` metrics.
  - Monitor NRR rule-level health and aggregate readiness metrics.

## Metrics Available During Scale Tests

During scale tests you will see both:

  - **NRR custom metrics** exposed by the controller.
  - **controller-runtime / process metrics** exposed by the manager and scraped by Prometheus.

### NRR Custom Metrics (The "Mega-Scale" Stack)

NRR uses an **Aggregate-First** telemetry strategy. This means we track the state of the cluster without introducing per-node metric labels, ensuring the controller remains lean as the cluster grows.

The following controller metrics are currently registered:

  - `node_readiness_rules_total`
  - `node_readiness_taint_operations_total{rule, operation}`
  - `node_readiness_evaluation_duration_seconds{rule}`
  - `node_readiness_failures_total{rule, reason}`
  - `node_readiness_bootstrap_completed_total{rule}`
  - `node_readiness_reconciliation_latency_seconds{rule, operation}`
  - `node_readiness_bootstrap_duration_seconds{rule}`
  - `node_readiness_nodes_by_state{rule, state}`
  - `node_readiness_rule_last_reconciliation_timestamp_seconds{rule}`

## Prerequisites

Ensure the following tools are installed:

```bash
kind version
kubectl version --client
helm version
podman --version   # or docker --version
jq --version
bc --version
```

Container runtime support:

  - **Podman** is the default in the Makefile
  - **Docker** is also supported via `CONTAINER_TOOL=docker`

## Quick Start

### Option 1: Makefile

```bash
cd hack/test-workloads/scale

# Full setup: cluster + controller + monitoring
make setup

# Run a test
make test NODE_COUNT=1000 RULE_COUNT=3

# Open Grafana
make dashboard

# Open Prometheus
make prometheus

# Inspect the controller's /metrics endpoint output
make metrics
```

Using Docker instead of Podman:

```bash
cd hack/test-workloads/scale
make setup CONTAINER_TOOL=docker
make test NODE_COUNT=1000 RULE_COUNT=3 CONTAINER_TOOL=docker
```

### Option 2: Script

```bash
cd hack/test-workloads/scale

# Setup monitoring stack
./setup-monitoring.sh

# Run scale test in another terminal
./scale-test.sh 1000 3
```

## Detailed Setup

### Container runtime configuration

The Makefile supports both Podman and Docker.

#### Podman

```bash
make setup
make test NODE_COUNT=1000 RULE_COUNT=3
```

#### Docker

```bash
make setup CONTAINER_TOOL=docker
make test NODE_COUNT=1000 RULE_COUNT=3 CONTAINER_TOOL=docker
```

#### Show current configuration

```bash
make info
```

### Available Make targets

```bash
make help
make verify
make info
make status
```

Key targets:

| Target | Description |
| --- | --- |
| `make setup` | Create cluster, install controller, install monitoring |
| `make test NODE_COUNT=1000 RULE_COUNT=3` | Run scale test |
| `make test-quick` | 100 nodes, 1 rule |
| `make test-medium` | 500 nodes, 2 rules |
| `make test-large` | 1000 nodes, 3 rules |
| `make test-xlarge` | 5000 nodes, 5 rules |
| `make dashboard` | Open Grafana |
| `make prometheus` | Open Prometheus |
| `make metrics` | Print the controller `/metrics` output via the Kubernetes Service proxy |
| `make logs` | Follow controller logs |
| `make status` | Show status of cluster, controller, monitoring, and port-forwarding |
| `make clean` | Remove everything |

### What `make setup` does

`make setup` runs:

1.  `create-cluster`
2.  `install-controller`
3.  `install-monitoring`

Controller installation enables the metrics endpoint and deploys the controller into the `nrr-system` namespace.

Monitoring installation:

- installs or updates `kube-prometheus-stack`
- creates the `monitoring` namespace
- applies `servicemonitor.yaml`
- configures Prometheus with a `5s` scrape interval for the stack
- disables `nodeExporter` in this scale-test setup
- starts local port-forwards for Grafana and Prometheus

### Metrics scraping configuration

The scale setup uses `hack/test-workloads/scale/servicemonitor.yaml`.

Current behavior:

  - scrapes the controller Service in namespace `nrr-system`
  - matches Service labels:
      - `control-plane: controller-manager`
      - `app.kubernetes.io/name: nrrcontroller`
  - scrapes endpoint:
      - port: `http`
      - scheme: `http`
      - interval: `5s`

This matches the scale-test setup, which deploys the controller with metrics enabled over HTTP.

## Import the Grafana Dashboard

1. Open Grafana at `http://localhost:3000`
2. Login with:
   - username: `admin`
   - password: `admin` when using the Makefile setup
   - password from script output when using `setup-monitoring.sh`
3. Import `hack/test-workloads/scale/grafana-dashboard.json`
4. Select Prometheus as the datasource

The dashboard JSON in this directory is the source of truth for the available panels.

Current dashboard highlights:

- **NRR Ready Nodes (%)**: percentage of nodes currently in NRR `ready` state
- **SLI: Fast Evaluations (% under 50ms)**: percentage of evaluations completing within 50ms
- **Bootstrap Completions**: total number of completed bootstrap events
- **Nodes by Readiness State**: aggregate counts for `ready`, `not_ready`, and `bootstrapping`
- **Nodes by Rule and State**: readiness-state breakdown per rule
- **Reconciliation Latency (P50/P95/P99)**: latency percentiles broken out by operation label
- **Evaluation Rate by Rule**: how actively each rule is being evaluated
- **Taint Operations (Throughput)**: add/remove operation rate
- **Failures & Errors**: failure rate by reason
- **Rule Reconciliation Age**: time since each rule last reconciled
- **Workqueue Depth (Backlog)**: controller backlog indicator
- **Controller Memory Usage** and **Controller CPU Usage** with both container-level and process-level visibility where available
- **Bootstrap Duration by Rule (P95)**: bootstrap latency broken out per rule
- **Bootstrap Duration Rate / Samples**: indicates whether bootstrap duration histograms currently have sample volume
- **Total Taint Operations**: cumulative add/remove operations over the selected time range

## Run Scale Tests

### Using the Makefile

```bash
make test NODE_COUNT=1000 RULE_COUNT=3

make test-quick
make test-medium
make test-large
make test-xlarge
```

### Using the script directly

```bash
./scale-test.sh <NODE_COUNT> <RULE_COUNT>

./scale-test.sh 100 1
./scale-test.sh 1000 3
./scale-test.sh 5000 5
```

### What the test does

The test workflow is:

1.  clean up old test artifacts
2.  create one or more `NodeReadinessRule` objects
3.  create fake KWOK nodes
4.  wait for NRR to apply taints
5.  patch node conditions so rules become satisfied
6.  wait for NRR to remove taints
7.  print timing and throughput results

## Monitoring Queries

Use these in Prometheus while running scale tests to validate controller performance.

### Evaluation Performance

Measures the percentage of evaluations completing within 50ms.

```promql
sum(rate(node_readiness_evaluation_duration_seconds_bucket{le="0.05"}[5m])) /
sum(rate(node_readiness_evaluation_duration_seconds_count[5m])) * 100
```

### End-to-End Reconciliation Latency (P99)

How long does it take NRR to react to a condition change in the cluster?

```promql
histogram_quantile(0.99, 
  sum by (le, operation) (rate(node_readiness_reconciliation_latency_seconds_bucket[5m]))
)
```

### Cluster Readiness Overview

Safely aggregate node health without cardinality explosions.

```promql
sum by (state) (node_readiness_nodes_by_state)
```

### Controller Freshness (Is it stuck?)

```promql
# Alert if any rule has not reconciled in the last 120 seconds
(time() - node_readiness_rule_last_reconciliation_timestamp_seconds) > 120
```

### Failure Rate

```promql
sum by (reason) (rate(node_readiness_failures_total[5m]))
```

### Controller Resource Usage

```promql
process_resident_memory_bytes
rate(process_cpu_seconds_total[5m])
```

### Controller-runtime Metrics

```promql
sum(rate(controller_runtime_reconcile_total[5m]))
sum(rate(controller_runtime_reconcile_errors_total[5m]))
workqueue_depth
```

## Interpreting Results

### Good signals during a healthy scale run

- **Stable memory profile:** controller memory should stay relatively stable for a given test size.
- **Evaluation performance:** the fast-evaluations panel tracks the percentage of evaluations completing within 50ms.
- **Throughput spikes:** taint operations should spike during node creation and condition patching, then fall back down.
- **Clean node transitions:** `node_readiness_nodes_by_state` should move from `not_ready` or `bootstrapping` toward `ready`.
- **Per-rule visibility:** `Nodes by Rule and State` and `Evaluation Rate by Rule` should make it obvious if one rule is lagging behind the others.
- **Bootstrap duration:** bootstrap duration panels reflect the end-to-end time for nodes to reach bootstrap completion.
- **Bootstrap completion growth:** the bootstrap completions stat should rise as nodes complete bootstrap-only workflows.
- **Low failure rate:** `node_readiness_failures_total` should remain low or flat in healthy runs.

### Important note on ready percentage

`NRR Ready Nodes (%)` is based on **NRR aggregate state**, not the Kubernetes `Ready=True` node condition.

During scale tests, it is normal for this panel to stay low or at `0%` during the taint-add phase because the test intentionally creates nodes before satisfying the custom readiness conditions. It should increase after the condition patching phase completes.

### Signals to investigate

- **Rising workqueue depth:** indicates the controller cannot keep up with node events.
- **High sustained latency percentiles:** suggests API pressure or reconciliation bottlenecks.
- **Memory growth across repeated runs:** may indicate a leak or excessive retained state.
- **Bootstrap duration getting worse with scale:** suggests the controller or API server is struggling to complete bootstrap-only workflows promptly.
- **Rule lag continuously increasing:** investigate reconcile health if `node_readiness_rule_last_reconciliation_timestamp_seconds` stops advancing while work remains.

### Example validation checklist

After a `make test-large` run:

  - verify taint operations occurred: `sum(node_readiness_taint_operations_total)`
  - verify evaluations occurred: `sum(node_readiness_evaluation_duration_seconds_count)`
  - verify no unexpected sustained failures: `sum(rate(node_readiness_failures_total[5m]))`
  - verify aggregate node state moved as expected: `node_readiness_nodes_by_state`

## Troubleshooting

### Metrics are missing in Prometheus

Check that the controller is running:

```bash
kubectl get pods -n nrr-system
kubectl logs -n nrr-system -l control-plane=controller-manager --tail=100
```

Check that the ServiceMonitor exists:

```bash
kubectl get servicemonitor -n monitoring
kubectl get servicemonitor -n monitoring node-readiness-controller-monitor -o yaml
```

Check Prometheus targets:

```bash
make prometheus
```

Then inspect `http://localhost:9090/targets`.

### Metrics endpoint not reachable

The scale setup scrapes the HTTP metrics endpoint through the ServiceMonitor. Verify the Service exists and exposes port `http`:

```bash
kubectl get svc -n nrr-system
kubectl get svc -n nrr-system metrics-service -o yaml
```

### Dashboard shows no data

  - ensure the Grafana time range includes the test interval
  - verify the Prometheus datasource is healthy
  - confirm the imported dashboard uses the Prometheus datasource
  - query the metrics directly in Prometheus first

### Scale test hangs

```bash
kubectl logs -n nrr-system -l control-plane=controller-manager -f
kubectl get nodes -l kwok.x-k8s.io/node=fake --watch
kubectl get nodereadinessrules
```

### Port forwarding fails

```bash
lsof -i :3000
lsof -i :9090
```

Then restart:

```bash
kubectl port-forward -n monitoring svc/prom-stack-grafana 3000:80 &
kubectl port-forward -n monitoring svc/prom-stack-kube-prometheus-prometheus 9090:9090 &
```

### Podman image build fails

```bash
cd ../../../
make podman-build
podman images | grep controller
```

## Cleanup

### Makefile targets

```bash
make clean-test
make clean-monitoring
make clean-controller
make clean-cluster
make clean
```

### Manual cleanup

```bash
./cleanup-kwok-nodes-rules.sh

kubectl delete nodereadinessrules -l scale-test=true
kubectl delete nodes -l kwok.x-k8s.io/node=fake

pkill -f "port-forward.*grafana"
pkill -f "port-forward.*prometheus"

kind delete cluster --name nrr-test
helm uninstall prom-stack -n monitoring
```

## Advanced Usage

### Monitor etcd size

```bash
make etcd-size
```

### View logs, metrics, and component status

```bash
make logs
make metrics
make status
```

### Inspect the current setup

```bash
make verify
make info
```

## Additional Resources

- [Monitoring Operations Guide](../../../docs/book/src/operations/monitoring.md)
- [Main Project README](../../../README.md)
- [Architecture Draft](../../../docs/architecture.draft.md)
- [API Reference](../../../docs/book/src/reference/api-spec.md)

-----

Happy testing\!