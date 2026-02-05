# Security Agent Readiness Guardrail

This guide demonstrates how to use the Node Readiness Controller to prevent workloads from being scheduled on a node until a security agent (for example, [Falco](https://github.com/falcosecurity/falco)) is fully initialized and actively monitoring the node.

## The Problem

In many Kubernetes clusters, security agents are deployed as DaemonSets. When a new node joins the cluster, there is a race condition:
1. A new node joins the cluster and is marked `Ready` by the kubelet.
2. The scheduler sees the node as `Ready` and considers the node eligible for workloads.
3. However, the security agent on that node may still be starting or initializing.

**Result**: Application workloads may start running before security monitoring is active, creating a blind spot where runtime threats, policy violations, or anomalous behavior may go undetected.

## The Solution

We can use the Node Readiness Controller to enforce a security readiness guardrail:
1. **Taint** the node with a [startup taint](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/)`readiness.k8s.io/SecurityReady=pending:NoSchedule` as soon as it joins the cluster.
2. **Monitor** the security agent’s readiness using a sidecar and expose it as a Node Condition.
3. **Untaint** the node only after the security agent reports that it is ready.

## Step-by-Step Guide (Falco Example)

This example uses **Falco** as a representative security agent, but the same pattern applies to any node-level security or monitoring agent.

> **Note**:  All manifests referenced in this guide are available in the [`examples/security-agent-readiness`](https://github.com/kubernetes-sigs/node-readiness-controller/tree/main/examples/security-agent-readiness) directory.



### 1. Deploy the Readiness Condition Reporter

To bridge the security agent’s internal health signal to Kubernetes, we deploy a readiness reporter that updates a Node Condition. This reporter is typically deployed as a sidecar container in the Falco DaemonSet.

This sidecar periodically checks Falco's local health endpoint (`http://localhost:8765/healthz`) and updates a Node Condition `security.k8s.io/FalcoReady`.

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
      value: "security.k8s.io/FalcoReady"   # Update the right condition
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

> Note: In this example, the security agent’s health is monitored by a side-car, so the reporter’s lifecycle is the same as the pod lifecycle. If the Falco pod is crashlooping, the sidecar will not run and cannot report readiness. For robust `continuous` readiness reporting, the reporter should be deployed `external` to the pod.

### 2. Grant Permissions (RBAC)

The readiness reporter sidecar needs permission to update the Node object's status to publish readiness information.

```yaml
# security-agent-node-status-rbac.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: node-status-patch-role
rules:
- apiGroups: [""]
  resources: ["nodes/status"]
  verbs: ["patch", "update"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: security-agent-node-status-patch-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: node-status-patch-role
subjects:
# Bind to security agent's ServiceAccount
- kind: ServiceAccount
  name: falco
  namespace: kube-system
```

### 3. Create the Node Readiness Rule

Next, define a NodeReadinessRule that enforces the security readiness requirement. This rule instructs the controller: *"Keep the `readiness.k8s.io/SecurityReady` taint on the node until `security.k8s.io/FalcoReady` condition becomes True."*

```yaml
# security-agent-readiness-rule.yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: security-agent-readiness-rule
spec:
  # Conditions that must be satisfied before the taint is removed
  conditions:
    - type: "security.k8s.io/FalcoReady"
      requiredStatus: "True"

  # Taint managed by this rule
  taint:
    key: "readiness.k8s.io/SecurityReady"
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

## How to Apply

1.  **Create the Node Readiness Rule**:
```sh
   cd examples/security-agent-readiness
   kubectl apply -f security-agent-readiness-rule.yaml
   ```

2. **Install Falco and Apply the RBAC**:
```sh
chmod +x apply-falco.sh
sh apply-falco.sh
```

## Verification

To verify that the guardrail is working, add a new node to the cluster.

1. **Check the Node Taints**:
Immediately after the node joins, it should have the taint:
`readiness.k8s.io/SecurityReady=pending:NoSchedule`.

2. **Check Node Conditions**:
Observe the node’s conditions. You will initially see `security.k8s.io/FalcoReady` as `False` or missing. Once Falco initializes, the sidecar reporter updates the condition to `True`.

3. **Check Taint Removal**:
As soon as the condition becomes `True`, the Node Readiness Controller removes the taint, allowing workloads to be scheduled on the node.
