# CNI Readiness

In many Kubernetes clusters, the CNI plugin runs as a DaemonSet. When a new node joins the cluster, there is a race condition:
1.  The Node object is created and marked `Ready` by the Kubelet.
2.  The Scheduler sees the node as `Ready` and schedules application pods.
3.  However, the CNI DaemonSet might still be initializing networking on that node.

This guide demonstrates how to use the Node Readiness Controller to prevent pods from being scheduled on a node until the Container Network Interface (CNI) plugin (e.g., Calico) is fully initialized and ready.

The high-level steps are:
1.  Node is bootstrapped with a [startup taint](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) `readiness.k8s.io/NetworkReady=pending:NoSchedule` immediately upon joining.
2.  A reporter DaemonSet is deployed to monitor the CNI's health and report it to the API server as node-condition (`projectcalico.org/CalicoReady`). 
3. Node Readiness Controller will untaint the node only when the CNI reports it is ready.

## Step-by-Step Guide

This example uses **Calico**, but the pattern applies to any CNI.

> **Note**: You can find all the manifests used in this guide in the [`examples/cni-readiness`](https://github.com/kubernetes-sigs/node-readiness-controller/tree/main/examples/cni-readiness) directory.

### 1. Deploy the Readiness Condition Reporter

We need to bridge Calico's internal health status to a Kubernetes Node Condition. We will deploy a **reporter DaemonSet** that runs on every node.

This reporter checks Calico's local health endpoint (`http://localhost:9099/readiness`) and updates a node condition `projectcalico.org/CalicoReady`.

Using a separate DaemonSet instead of a sidecar ensures that readiness reporting works even if the CNI pod is crashlooping or failing to start containers.

**Deploy the Reporter DaemonSet:**

```yaml
# cni-reporter-ds.yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: cni-reporter
  namespace: kube-system
spec:
  # ...
  template:
    spec:
      hostNetwork: true
      serviceAccountName: cni-reporter
      tolerations:
      - operator: Exists
      containers:
      - name: cni-status-patcher
        image: registry.k8s.io/node-readiness-controller/node-readiness-reporter:v0.1.1
        env:
          - name: CHECK_ENDPOINT
            value: "http://localhost:9099/readiness"
          - name: CONDITION_TYPE
            value: "projectcalico.org/CalicoReady"
```

### 2. Grant Permissions (RBAC)

The reporter needs permission to update the Node object's status.

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
# Bind to CNI Reporter's ServiceAccount
- kind: ServiceAccount
  name: cni-reporter
  namespace: kube-system
```

### 3. Create the Node Readiness Rule

Now define the rule that enforces the requirement. This tells the controller: *"Keep the `readiness.k8s.io/NetworkReady` taint on the node until `projectcalico.org/CalicoReady` is True."*

```yaml
# network-readiness-rule.yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: network-readiness-rule
spec:
  # The condition(s) to monitor
  conditions:
    - type: "projectcalico.org/CalicoReady"
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
    Watch the node conditions. You will initially see `projectcalico.org/CalicoReady` as `False` or missing.
    Once Calico starts, the reporter will update it to `True`.

3.  **Check Taint Removal**:
    As soon as the condition becomes `True`, the Node Readiness Controller will remove the taint, and workloads will be scheduled.