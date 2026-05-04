# NPD + Descheduler Remediation Example

This example demonstrates an automated self-healing loop using:

1. **Node Problem Detector (NPD)** — detects node-level issues via custom health checks
2. **Node Readiness Controller (NRC)** — watches NPD conditions and manages taints
3. **Descheduler** — evicts pods from tainted nodes so they reschedule to healthy ones

### Flow

```
NPD detects failure → Sets NodeCondition → NRC adds taint → Descheduler evicts pods → Scheduler reschedules
```

When the issue resolves, NPD clears the condition, NRC removes the taint, and the node becomes schedulable again.

### How to run

```sh
# 1. Create kind cluster
kind create cluster --config examples/npd-descheduler-remediation/kind-cluster-config.yaml

# 2. Deploy the Node Readiness Controller
make deploy

# 3. Deploy NPD
kubectl apply -f examples/npd-descheduler-remediation/npd-rbac.yaml
kubectl apply -f examples/npd-descheduler-remediation/npd-custom-plugin-config.yaml
kubectl apply -f examples/npd-descheduler-remediation/npd-daemonset.yaml

# 4. Apply the NodeReadinessRule
kubectl apply -f examples/npd-descheduler-remediation/node-readiness-rule.yaml

# 5. Deploy Descheduler
kubectl apply -f examples/npd-descheduler-remediation/descheduler-rbac.yaml
kubectl apply -f examples/npd-descheduler-remediation/descheduler-policy.yaml
kubectl apply -f examples/npd-descheduler-remediation/descheduler-deployment.yaml

# 6. Deploy sample workload
kubectl apply -f examples/npd-descheduler-remediation/sample-workload.yaml

# 7. Verify
kubectl get nodes -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints
kubectl get nodereadinessrule my-component-readiness-rule -o yaml
```
