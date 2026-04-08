# NPD Prober — Custom Plugin for Node Problem Detector

A lightweight Go binary that acts as a [node-problem-detector (NPD)](https://github.com/kubernetes/node-problem-detector) custom plugin. It performs HTTP or TCP probes using kubelet-style semantics and returns NPD-compatible exit codes.

## How It Works

```
NPD executes npd-prober binary
        │
        ▼
  Probe target (HTTP GET or TCP connect)
        │
        ▼
  Exit code: 0=OK, 1=NonOK, 2=Unknown
        │
        ▼
  NPD sets NodeCondition (e.g. ServiceReadiness=True/False)
        │
        ▼
  Node Readiness Controller watches condition
        │
        ▼
  NRC manages taint (e.g. readiness.k8s.io/ServiceReady)
```

## CLI Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--probe-type` | Probe type: `http` or `tcp` | (required) |
| `--http-url` | URL for HTTP probe | (required for `http`) |
| `--tcp-addr` | Address (`host:port`) for TCP probe | (required for `tcp`) |
| `--timeout` | Probe timeout | `5s` |

## Exit Codes

| Code | Meaning | NPD Interpretation |
|------|---------|-------------------|
| 0 | OK / Healthy | Condition transitions to healthy state |
| 1 | NonOK / Unhealthy | Condition transitions to unhealthy state |
| 2 | Unknown | Configuration error, condition unchanged |

## Build

```bash
go build -o npd-prober ./examples/npd-prober/
```

## Usage

HTTP probe:
```bash
./npd-prober --probe-type=http --http-url=http://localhost:8080/healthz
```

TCP probe:
```bash
./npd-prober --probe-type=tcp --tcp-addr=localhost:5432 --timeout=3s
```

## NPD Configuration

See [`npd-config.json`](npd-config.json) for an example NPD custom plugin monitor configuration. Place it in your NPD config directory and ensure the prober binary is accessible at the configured path.

## Node Readiness Controller Integration

See [`node-readiness-rule.yaml`](node-readiness-rule.yaml) for an example `NodeReadinessRule` that watches the condition NPD sets and manages a taint accordingly:

```bash
kubectl apply -f examples/npd-prober/node-readiness-rule.yaml
```

This creates a rule that:
1. Watches nodes for the `ServiceReadiness` condition (set by NPD via the prober)
2. Manages the `readiness.k8s.io/ServiceReady=pending:NoSchedule` taint
3. Removes the taint when the condition becomes `True`
4. Re-adds the taint when the condition becomes `False` (continuous enforcement)
