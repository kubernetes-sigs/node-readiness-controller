# Demo Environment Setup

This demo environment is used to generate metrics for the Grafana dashboard during development and testing.

## Rules

Three rules, defined in [hack/test-workloads/](hack/test-workloads/)

### GPU Driver Readiness

Simulates GPU nodes waiting for the NVIDIA driver before becoming ready.

- Mode: `bootstrap-only`
- Behavior: Intentionally matches 0 nodes to simulate a misconfigured rule (for example, an incorrect node label).

---

### CSI Registration

Simulates nodes waiting for a CSI driver to register during startup.

- Mode: `bootstrap-only`
- Applies to **8 nodes**
- **6 nodes** become ready after different delays.
- **2 nodes** remain blocked to simulate a failed bootstrap.

This rule provides data for:

- Blocked Nodes
- Fleet Availability
- Bootstrap latency metrics

---

### Security Agent Check-in

Simulates a security agent reporting node health.

- Mode: continuous
- Applies to 12 nodes
- Behavior: One node briefly becomes unhealthy before recovering to demonstrate continuous reconciliation.

---

# Reproducing the Demo

From a clean cluster:

```bash
kubectl delete nodereadinessrule \
  gpu-driver-readiness-rule \
  csi-registration-rule \
  security-agent-checkin-rule \
  --ignore-not-found

# Once the demo cluster and Grafana are running, run:
./hack/test-workloads/demo-stagger-conditions.sh
```

## Expected Dashboard Values


| Panel               | Expected Value |
| ------------------- | -------------- |
| Total Managed Nodes | 20             |
| Blocked Nodes       | 2              |
| Fleet Availability  | 90%            |
| Zero-Match Rules    | 1              |

> **Note:** This is the baseline demo environment used for the Fleet Overview section. Additional scenarios may be added as later dashboard sections require them.