# Installation

Follow this guide to install the Node Readiness Controller in your Kubernetes cluster.

## Prerequisites

If you plan to use the `install-full.yaml` option (which includes secure metrics and the validating admission webhook), you must first have [cert-manager](https://cert-manager.io/docs/installation/) installed in your cluster.

## Deployment Options

### Option 1: Official Release (Recommended)

First, to install the CRDs, apply the `crds.yaml` manifest:

```sh
# Replace with the desired version
VERSION=v0.1.1
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/${VERSION}/crds.yaml
kubectl wait --for condition=established --timeout=30s crd/nodereadinessrules.readiness.node.x-k8s.io

```

#### 2. Install the Controller

Choose one of the two following manifests based on your requirements:

| Manifest | Contents | Prerequisites |
| :--- | :--- | :--- |
| **`install.yaml`** | Core Controller | None |
| **`install-full.yaml`** | Core Controller + Metrics (Secure) + Validation Webhook | `cert-manager` |

**Standard Installation (Minimal):**
The simplest way to deploy the controller with no external dependencies.

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/${VERSION}/install.yaml
```

**Full Installation (Production Ready):**
Includes secure metrics (TLS-protected) and validating webhooks for rule conflict prevention. **Requires [cert-manager](https://cert-manager.io/docs/installation/)** to be installed in your cluster.

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/${VERSION}/install-full.yaml
```

This will deploy the controller into the `nrr-system` namespace on any available node in your cluster.

#### Controller priority

The controller is deployed with `system-cluster-critical` priority to prevent eviction during node resource pressure.

If it gets evicted during resource pressure, nodes can't transition to Ready state, blocking all workload scheduling cluster-wide.

This is the priority class used by other critical cluster components (eg: core-dns).

#### Images

The official releases use multi-arch images (AMD64, Arm64) and are available at `registry.k8s.io/node-readiness-controller/node-readiness-controller`

```sh
REPO="registry.k8s.io/node-readiness-controller/node-readiness-controller"
TAG=$(skopeo list-tags docker://$REPO | jq .'Tags[-1]' | tr -d '"')
docker pull $REPO:$TAG
```
### Option 2: Advanced Deployment (Kustomize)

If you need deeper customization, you can use Kustomize directly from the source.

```sh
# 1. Install CRDs
kubectl apply -k config/crd

# 2. Deploy Controller with default configuration
kubectl apply -k config/default
```

You can enable optional components (Metrics, TLS, Webhook) by creating a `kustomization.yaml` that includes the relevant components from the `config/` directory. For reference on how these components can be combined, see the `deploy-with-metrics`, `deploy-with-tls`, `deploy-with-webhook`, and `deploy-full` targets in the projects [`Makefile`](https://github.com/kubernetes-sigs/node-readiness-controller/blob/main/Makefile).

---

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
    
    # Or if using the full manifest
    kubectl delete -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/${VERSION}/install-full.yaml

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
