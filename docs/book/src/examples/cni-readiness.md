# CNI Readiness

In many Kubernetes clusters, the CNI plugin runs as a DaemonSet. When a new node joins the cluster, there is a race condition:
1.  The Node object is created and marked `Ready` by the Kubelet.
2.  The Scheduler sees the node as `Ready` and schedules application pods.
3.  However, the CNI DaemonSet might still be initializing networking on that node.

This guide demonstrates how to use the Node Readiness Controller to prevent pods from being scheduled on a node until the Container Network Interface (CNI) plugin (e.g., Calico) is fully initialized and ready.

The high-level steps are:
1.  Node is bootstrapped with a [startup taint](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) `readiness.k8s.io/NetworkReady=pending:NoSchedule` immediately upon joining.
2.  A sidecar is patched to the cni-agent to monitor the CNI's health and report it to the API server as node-condition (`network.k8s.io/CalicoReady`). 
3. Node Readiness Controller will untaint the node only when the CNI reports it is ready.

## Step-by-Step Guide

This example uses **Calico**, but the pattern applies to any CNI.

> **Note**: You can find all the manifests used in this guide in the [`examples/cni-readiness`](https://github.com/kubernetes-sigs/node-readiness-controller/tree/main/examples/cni-readiness) directory.

### 1. Deploy the Readiness Condition Reporter

We need to bridge Calico's internal health status to a Kubernetes Node Condition. We will add a **sidecar container** to the Calico DaemonSet.

This sidecar checks Calico's local health endpoint (`http://localhost:9099/readiness`) and updates a node condition `network.k8s.io/CalicoReady`.

**Patch your Calico DaemonSet:**

```yaml
# cni-patcher-sidecar.yaml
- name: cni-status-patcher
  image: registry.k8s.io/node-readiness-controller/node-readiness-reporter:v0.1.1
  imagePullPolicy: IfNotPresent
  env:
    - name: NODE_NAME
      valueFrom:
        fieldRef:
          fieldPath: spec.nodeName
    - name: CHECK_ENDPOINT
      value: "http://localhost:9099/readiness" # update to your CNI health endpoint
    - name: CONDITION_TYPE
      value: "network.k8s.io/CalicoReady"     # update this node condition
    - name: CHECK_INTERVAL
      value: "15s"
  resources:
    limits:
      cpu: "10m"
      memory: "32Mi"
    requests:
      cpu: "10m"
      memory: "32Mi"
```

  > Note: In this example, the CNI pod health is monitored by a side-car, so watcher's lifecycle is same as the pod lifecycle.
  If the Calico pod is crashlooping, the sidecar will not run and cannot report readiness. For robust 'continuous' readiness reporting, the watcher should be 'external' to the pod.

### 2. Grant Permissions (RBAC)

The sidecar needs permission to update the Node object's status.

```yaml
# calico-rbac-node-status-patch-role.yaml
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
  name: calico-node-status-patch-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: node-status-patch-role
subjects:
# Bind to CNI's ServiceAccount
- kind: ServiceAccount
  name: calico-node
  namespace: kube-system
```

### 3. Create the Node Readiness Rule

Now define the rule that enforces the requirement. This tells the controller: *"Keep the `readiness.k8s.io/NetworkReady` taint on the node until `network.k8s.io/CalicoReady` is True."*

```yaml
# network-readiness-rule.yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: network-readiness-rule
spec:
  # The condition(s) to monitor
  conditions:
    - type: "network.k8s.io/CalicoReady"
      requiredStatus: "True"
  
  # The taint to manage
  taint:
    key: "readiness.k8s.io/NetworkReady"
    effect: "NoSchedule"
    value: "pending"
  
  # "bootstrap-only" means: once the CNI is ready once, we stop enforcing.
  enforcementMode: "bootstrap-only"
  
  # Update to target only the nodes that need to be protected by this guardrail
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/worker: ""
```

## Test scripts

1.  **Create the Readiness Rule**:
    ```sh
    cd examples/cni-readiness
    kubectl apply -f network-readiness-rule.yaml
    ```

2.  **Install Calico CNI and Apply the RBAC**:
    ```sh
    chmod +x apply-calico.sh
    sh apply-calico.sh
    ```


## Verification

To test this, add a new node to the cluster.

1.  **Check the Node Taints**:
    Immediately upon joining, the node should have the taint:
    `readiness.k8s.io/NetworkReady=pending:NoSchedule`.

2.  **Check Node Conditions**:
    Watch the node conditions. You will initially see `network.k8s.io/CalicoReady` as `False` or missing.
    Once Calico starts, the sidecar will update it to `True`.

3.  **Check Taint Removal**:
    As soon as the condition becomes `True`, the Node Readiness Controller will remove the taint, and workloads will be scheduled.
