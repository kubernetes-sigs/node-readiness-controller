# Installation

Follow this guide to install the Node Readiness Controller in your Kubernetes cluster.

## Deployment Options

### Option 1: Install Official Release (Recommended)

The easiest way to get started is by applying the official release manifests.

First, to install the CRDs, apply the `crds.yaml` manifest:

```sh
# Replace with the desired version
VERSION=v0.1.1
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/${VERSION}/crds.yaml
kubectl wait --for condition=established --timeout=30s crd/nodereadinessrules.readiness.node.x-k8s.io

```

To install the controller, apply the `install.yaml` manifest:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/${VERSION}/install.yaml
```

This will deploy the controller into the `nrr-system` namespace on any available node in your cluster.

#### Controller priority

The controller is deployed with `system-cluster-critical` priority to prevent eviction during node resource pressure.

If it gets evicted during resource pressure, nodes can't transition to Ready state, blocking all workload scheduling cluster-wide.

This is the priority class used by other critical cluster components (eg: core-dns).

**Images**: The official releases use multi-arch images (AMD64, Arm64).

### Option 2: Deploy Using Kustomize

If you have cloned the repository and want to deploy from source, you can use Kustomize.

```sh
# 1. Install Custom Resource Definitions (CRDs)
kubectl apply -k config/crd

# 2. Deploy Controller and RBAC
kubectl apply -k config/default
```

## Verification

After installation, verify that the controller is running successfully.

1.  **Check Pod Status**:
    ```sh
    kubectl get pods -n nrr-system
    ```
    You should see a pod named `nrr-controller-manager-...` in `Running` status.

2.  **Check Logs**:
    ```sh
    kubectl logs -n nrr-system -l control-plane=controller-manager
    ```
    Look for "Starting EventSource" or "Starting Controller" messages indicating the manager is active.

3.  **Verify CRDs**:
    ```sh
    kubectl get crd nodereadinessrules.readiness.node.x-k8s.io
    ```

## Uninstallation

> **IMPORTANT**: Follow this order to avoid "stuck" resources.

The controller uses a **finalizer** (`readiness.node.x-k8s.io/cleanup-taints`) on `NodeReadinessRule` resources to ensure taints are safely removed from nodes before a rule is deleted.

**You must delete all rule objects *before* deleting the controller.**

1.  **Delete all Rules**:
    ```sh
    kubectl delete nodereadinessrules --all
    ```
    *Wait for this command to complete.* This ensures the running controller removes its taints from your nodes.

2.  **Uninstall Controller**:
    ```sh
    # If installed via release manifest
    kubectl delete -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/${VERSION}/install.yaml

    # OR if using Kustomize
    kubectl delete -k config/default
    ```

3.  **Uninstall CRDs** (Optional):
    ```sh
    kubectl delete -k config/crd
    ```

### Recovering from Stuck Resources

If you accidentally deleted the controller *before* the rules, the `NodeReadinessRule` objects will get stuck in a `Terminating` state because the controller is needed to cleanup the taints and finalizers.

To force-delete them (this will require you to manually clean up the managed taints if any on your nodes):

```sh
# Patch the finalizer to remove it
kubectl patch nodereadinessrule <rule-name> -p '{"metadata":{"finalizers":[]}}' --type=merge
```

## Troubleshooting Deployment

**RBAC Permissions**
If the controller logs show "Forbidden" errors, verify the ClusterRole bindings:
```sh
kubectl describe clusterrole nrr-manager-role
```
It requires `nodes` (update/patch) and `nodereadinessrules` (all) access.

**Debug Logging**
To enable verbose logging for deeper investigation:
```sh
kubectl patch deployment -n nrr-system nrr-controller-manager \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--zap-log-level=debug"]}]}}}}'
```
