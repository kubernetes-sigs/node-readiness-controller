# Security Agent Readiness Guardrail

This guide demonstrates how to use the Node Readiness Controller to prevent workloads from being scheduled on a node until a security agent (for example, [Falco](https://github.com/falcosecurity/falco)) is fully initialized and actively monitoring the node.

## The Problem

In many Kubernetes clusters, security agents are deployed as DaemonSets. When a new node joins the cluster, there is a race condition:
1. A new node joins the cluster and is marked `Ready` by the kubelet.
2. The scheduler sees the node as `Ready` and considers the node eligible for workloads.
3. However, the security agent on that node may still be starting or initializing.

**Result**: Application workloads may start running before node is security compliant, creating a blind spot where runtime threats, policy violations, or anomalous behavior may go undetected.

## The Solution

We can use the Node Readiness Controller to enforce a security readiness guardrail:
1. **Taint** the node with a [startup taint](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) `readiness.k8s.io/security-agent-ready=pending:NoSchedule` as soon as it joins the cluster.
2. **Monitor** the security agent’s readiness using a sidecar and expose it as a Node Condition.
3. **Untaint** the node only after the security agent reports that it is ready.

## Step-by-Step Guide (Falco Example)

This example uses **Falco** as a representative security agent, but the same pattern applies to any node-level security or monitoring agent.

> **Note**:  All manifests referenced in this guide are available in the [`examples/security-agent-readiness`](https://github.com/kubernetes-sigs/node-readiness-controller/tree/main/examples/security-agent-readiness) directory.

### Prerequisites

**1. Node Readiness Controller:**

Before starting, ensure the Node Readiness Controller is deployed. See the [Installation Guide](../user-guide/installation.md) for details.

**2. Kubernetes Cluster with Worker Nodes:**

This example requires at least one worker node with the startup taint. For kind clusters, use the provided configuration:

```sh
kind create cluster --config examples/security-agent-readiness/kind-cluster-config.yaml
```

This creates a cluster with:
- 1 control-plane node
- 1 worker node pre-tainted with `readiness.k8s.io/security-agent-ready=pending:NoSchedule`

See [`examples/security-agent-readiness/kind-cluster-config.yaml`](../../../../examples/security-agent-readiness/kind-cluster-config.yaml) for details.

### 1. Deploy the Readiness Condition Reporter

To bridge the security agent's internal health signal to Kubernetes, we need to update a Node Condition. You have two options:

#### Option A: Using Node Readiness Reporter Sidecar

The reporter is deployed as a sidecar container in the Falco DaemonSet. This sidecar periodically checks Falco's local health endpoint (`http://localhost:8765/healthz`) and updates a Node Condition `falco.org/FalcoReady`.

**Patch your Falco DaemonSet:**

```yaml
# security-agent-reporter-sidecar.yaml
- name: security-status-patcher
  image: registry.k8s.io/node-readiness-controller/node-readiness-reporter:v0.1.1
  imagePullPolicy: IfNotPresent
  env:
    - name: NODE_NAME
      valueFrom:
        fieldRef:
          fieldPath: spec.nodeName
    - name: CHECK_ENDPOINT
      value: "http://localhost:8765/healthz" # Update the right security agent endpoint
    - name: CONDITION_TYPE
      value: "falco.org/FalcoReady"   # Update the right condition
    - name: CHECK_INTERVAL
      value: "5s"
  resources:
    limits:
      cpu: "10m"
      memory: "32Mi"
    requests:
      cpu: "10m"
      memory: "32Mi"
```

**Note:** The sidecar's lifecycle is tied to the Falco pod. If Falco crashes, the sidecar stops reporting. For more robust monitoring, see Option B below.

#### Option B: Using Node Problem Detector (More Robust)

If you already have Node Problem Detector (NPD) deployed or want robust monitoring that continues even if Falco crashes, use NPD with a custom plugin.

**Deploy NPD with Falco monitoring plugin:**

```yaml
# npd-falco-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: npd-falco-config
  namespace: falco
data:
  # NPD uses problem-oriented conditions (like MemoryPressure, DiskPressure).
  # falco.org/FalcoNotReady=False means Falco is healthy, falco.org/FalcoNotReady=True means there's an issue.
  falco-plugin.json: |
    {
      "plugin": "custom",
      "pluginConfig": {
        "invoke_interval": "10s",
        "timeout": "5s",
        "max_output_length": 80,
        "concurrency": 1
      },
      "source": "falco-monitor",
      "conditions": [
        {
          "type": "falco.org/FalcoNotReady",
          "reason": "FalcoHealthy",
          "message": "Falco security monitoring is functional"
        }
      ],
      "rules": [
        {
          "type": "permanent",
          "condition": "falco.org/FalcoNotReady",
          "reason": "FalcoNotDeployed",
          "path": "/config/plugin/check-falco.sh"
        }
      ]
    }
  
  check-falco.sh: |
    #!/bin/bash
    # Check if Falco is deployed and healthy
    # Exit 0 when healthy (FalcoNotReady=False, i.e., Falco IS ready)
    # Exit 1 when NOT healthy/deployed (FalcoNotReady=True, i.e., Falco is NOT ready)
    timeout 2 bash -c '</dev/tcp/127.0.0.1/8765' 2>/dev/null
    if [ $? -eq 0 ]; then
      exit 0  # Falco is healthy
    else
      echo "Falco is not deployed or not responding on port 8765"
      exit 1  # Falco has a problem
    fi
```

Then deploy NPD DaemonSet and RBAC. See complete NPD manifests in [`examples/security-agent-readiness/npd-variant/`](../../../../examples/security-agent-readiness/npd-variant/).

### 2. Create the Node Readiness Rule

Next, define a NodeReadinessRule that enforces the security readiness requirement.

**For Option A (Sidecar Reporter):**

```yaml
# nrr-variant/security-agent-readiness-rule.yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: security-agent-readiness-rule
spec:
  # Conditions that must be satisfied before the taint is removed
  conditions:
    - type: "falco.org/FalcoReady"
      requiredStatus: "True"

  # Taint managed by this rule
  taint:
    key: "readiness.k8s.io/security-agent-ready"
    effect: "NoSchedule"
    value: "pending"

  # "bootstrap-only" means: once the security agent is ready, we stop enforcing.
  # Use "continuous" mode if you want to taint the node if security agent crashes later. 
  enforcementMode: "bootstrap-only"

  # Update to target only the nodes that need to be protected by this guardrail
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/worker: ""
```

**For Option B (Node Problem Detector):**

```yaml
# npd-variant/security-agent-readiness-rule-npd.yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: security-agent-readiness-rule-npd
spec:
  # Conditions that must be satisfied before the taint is removed
  conditions:
    - type: "falco.org/FalcoNotReady"
      requiredStatus: "False"  # Remove taint when Falco is NOT NotReady (i.e., ready)

  # Taint managed by this rule
  taint:
    key: "readiness.k8s.io/security-agent-ready"
    effect: "NoSchedule"
    value: "pending"

  # "bootstrap-only" means: once the security agent is ready, we stop enforcing.
  # Use "continuous" mode if you want to re-taint the node if Falco crashes later.
  enforcementMode: "continuous"

  # Update to target only the nodes that need to be protected by this guardrail
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/worker: ""
```

## How to Apply

**For Option A (Sidecar Reporter):**

```sh
# Install Falco with sidecar reporter
USE_NRR=true examples/security-agent-readiness/setup-falco.sh

# Apply the NodeReadinessRule
kubectl apply -f examples/security-agent-readiness/nrr-variant/security-agent-readiness-rule.yaml

# Add toleration to Falco so it can start on tainted nodes
examples/security-agent-readiness/add-falco-toleration.sh
```

**For Option B (Node Problem Detector):**

```sh
# Install Falco with NPD monitoring
USE_NPD=true examples/security-agent-readiness/setup-falco.sh

# Apply the NodeReadinessRule for NPD
kubectl apply -f examples/security-agent-readiness/npd-variant/security-agent-readiness-rule-npd.yaml

# Add toleration to Falco so it can start on tainted nodes
examples/security-agent-readiness/add-falco-toleration.sh
```

## Verification

To verify that the guardrail is working, you need a tainted node. You have two options:

**Option 1: Manually taint an existing node:**

```sh
kubectl taint nodes <node-name> readiness.k8s.io/security-agent-ready=pending:NoSchedule
```

**Option 2: Configure nodes to register with taints at startup:**

For kind clusters, use kubeadm config patches. See [kind documentation on kubeadm config patches](https://kind.sigs.k8s.io/docs/user/configuration/#kubeadm-config-patches) for details.

---

Once the node is tainted:

1. **Check the Node Taints**:
   Verify the taint is applied:
   ```sh
   kubectl get nodes -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints
   ```
   Should show: `readiness.k8s.io/security-agent-ready=pending:NoSchedule`.

2. **Check Node Conditions**:
   
   **For Option A (Sidecar):**
   ```sh
   kubectl get node <node-name> -o jsonpath='{.status.conditions[?(@.type=="falco.org/FalcoReady")]}' | jq .
   ```
   You will initially see `falco.org/FalcoReady` as `False`. Once Falco initializes, it becomes `True`.

   **For Option B (NPD):**
   ```sh
   kubectl get node <node-name> -o jsonpath='{.status.conditions[?(@.type=="falco.org/FalcoNotReady")]}' | jq .
   ```
   You will initially see `falco.org/FalcoNotReady=True` (not ready). Once Falco is healthy, it becomes `falco.org/FalcoNotReady=False` (ready).


3. **Check Taint Removal**:
   As soon as the condition reaches the required status, the Node Readiness Controller removes the taint, allowing workloads to be scheduled on the node.
