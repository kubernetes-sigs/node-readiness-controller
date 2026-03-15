# Secure Status Reporting

By default, a readiness reporter's ServiceAccount is granted broad permissions to update the status of **any** Node in the cluster. If a node is compromised, an attacker can manipulate the readiness status of every other node.

[Constrained Impersonation (KEP-5284)](https://github.com/kubernetes/enhancements/issues/5284) solves this by allowing the reporter to impersonate only the Node it runs on. The API server enforces this at the authorization layer, so that no other node's status can be touched.

This guide walks through a CNI readiness example that uses constrained impersonation instead of broad RBAC. It is a hardened variant of the [CNI Readiness](./cni-readiness.md) example.

> **Prerequisites**: Kubernetes v1.35+ with the `ConstrainedImpersonation` feature gate enabled, or v1.36+ where it is Beta and enabled by default.

> **Note**: You can find all the manifests used in this guide in the [`examples/constrained-impersonation`](../../../../examples/constrained-impersonation) directory.

## Step-by-Step Guide

### 1. Create a Kind Cluster

Create a cluster with the `ConstrainedImpersonation` feature gate enabled and worker nodes that join with a startup taint:

```sh
kind create cluster \
  --config config/testing/kind/kind-constrained-impersonation-config.yaml \
  --image kindest/node:v1.35.0
```

### 2. Install the CRDs and Controller

```sh
make install
make deploy
```

### 3. Deploy the Example

```sh
cd examples/constrained-impersonation
kubectl apply -f .
```

### 4. RBAC Explained

The RBAC consists of two ClusterRoles:

**Impersonation role** — allows the reporter to impersonate its own Node:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: node-readiness-impersonator
rules:
- apiGroups: ["authentication.k8s.io"]
  resources: ["nodes"]
  verbs: ["impersonate:associated-node"]
```

**Constrained action role** — restricts what the impersonated identity can do:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: node-status-patcher-constrained
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["impersonate-on:associated-node:get"]
- apiGroups: [""]
  resources: ["nodes/status"]
  verbs: ["impersonate-on:associated-node:update"]
```

### 5. Verification

**Check that the reporter is running:**

```sh
kubectl -n kube-system get pods -l app=cni-reporter
```

**Check node conditions:**

```sh
kubectl get nodes -o custom-columns='NAME:.metadata.name,CALICO_READY:.status.conditions[?(@.type=="projectcalico.org/CalicoReady")].status'
```

### 6. Security Verification

**Verify the ServiceAccount has no direct permissions:**

```sh
kubectl auth can-i get nodes --as=system:serviceaccount:kube-system:cni-reporter
# no
kubectl auth can-i update nodes/status --as=system:serviceaccount:kube-system:cni-reporter
# no
```

The SA cannot read or update any node directly; all access goes through constrained impersonation.

**Verify the reporter can still update its own node (via impersonation):**

```sh
kubectl get nodes -o custom-columns='NAME:.metadata.name,CALICO_READY:.status.conditions[?(@.type=="projectcalico.org/CalicoReady")].status'
```

The `CalicoReady` condition should appear on every node.
This proves the reporter is successfully impersonating its local node identity and writing status.

## Comparison with Broad RBAC

For a deeper discussion, see [Security](../operations/security.md).
