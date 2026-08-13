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
### Option 2: Helm Chart

The chart lives in the repository under `charts/nrr-controller`. Published chart releases via `registry.k8s.io` OCI are still work in progress, so install it from a checkout for now.

```sh
git clone https://github.com/kubernetes-sigs/node-readiness-controller.git
cd node-readiness-controller

helm install nrr-controller ./charts/nrr-controller \
  --namespace nrr-system --create-namespace
```

Requires Helm 3.x. This deploys the controller with the same defaults as the standard manifest: leader election on, metrics off, and the validating webhook off.

For anything beyond a couple of overrides, keep your settings in a file instead of a long `--set` list:

```sh
helm show values ./charts/nrr-controller > custom-values.yaml

helm install nrr-controller ./charts/nrr-controller \
  --namespace nrr-system --create-namespace \
  -f custom-values.yaml
```

#### Optional components

Everything beyond the core controller is opt-in, matching the kustomize components.

| Feature | Values | Prerequisites |
| :--- | :--- | :--- |
| Metrics endpoint | `metrics.enabled=true` | None for plain HTTP |
| Metrics over TLS | `metrics.enabled=true`, `metrics.secure=true`, `certManager.enabled=true` | `cert-manager` |
| Validating webhook | `webhook.enabled=true`, `validatingWebhook.enabled=true`, `certManager.enabled=true` | `cert-manager` |

The webhook rejects rules whose taint key and effect collide with an existing rule over an overlapping node selector, so it is worth enabling in production.

```sh
helm install nrr-controller ./charts/nrr-controller \
  --namespace nrr-system --create-namespace \
  --set certManager.enabled=true \
  --set webhook.enabled=true \
  --set validatingWebhook.enabled=true \
  --set metrics.enabled=true \
  --set metrics.secure=true
```

Both `webhook.enabled` and `validatingWebhook.enabled` are needed. The first runs the webhook server in the controller and mounts its certificate, the second registers the `ValidatingWebhookConfiguration` with the API server.

#### Managing rules through the chart

`NodeReadinessRule` objects can be shipped with the release through the `nodeReadinessRules` value:

```yaml
nodeReadinessRules:
  - name: kube-proxy-unhealthy-noschedule
    enforcementMode: continuous
    conditions:
      - type: KubeProxyUnhealthy
        requiredStatus: "False"
    taint:
      key: readiness.k8s.io/KubeProxyUnhealthy
      value: "true"
      effect: NoSchedule
    nodeSelector:
      matchLabels:
        kubernetes.io/os: linux
```

`nodeSelector` is required on every entry. Set it explicitly, since an empty selector matches every node in the cluster.

> [!NOTE]
> With the validating webhook enabled, apply rules only once the controller is serving admission requests. On a first install the webhook is not ready while the rules in the same release are being created, so install the controller first and add the rules in a follow-up `helm upgrade`.

#### Upgrading

Pull the version of the chart you want and upgrade the release in place. Values you set at install time are carried over, so only pass the ones you are changing:

```sh
git pull

helm upgrade nrr-controller ./charts/nrr-controller \
  --namespace nrr-system \
  -f custom-values.yaml
```

`helm upgrade --install` works too if you want one command that handles both the first install and later upgrades.

Check what changed before applying it to a live cluster:

```sh
helm diff upgrade nrr-controller ./charts/nrr-controller --namespace nrr-system   # needs the helm-diff plugin
```

Read the CRD note below first. Helm will not update the CRD for you, so a chart bump that changes the schema needs that step done by hand.

#### CRD upgrades

Helm installs the CRD from the chart's `crds/` directory on first install only. It does not upgrade or remove it on `helm upgrade` or `helm uninstall`.

Before moving to a chart version that changes the `NodeReadinessRule` schema, apply the CRD yourself:

```sh
kubectl apply -f charts/nrr-controller/crds/nodereadinessrules.readiness.node.x-k8s.io.yaml
```

Skipping this leaves the old schema in place, and rules using newly added fields are rejected by the API server even though the controller supports them.

### Option 3: Advanced Deployment (Kustomize)

If you need deeper customization, you can use Kustomize directly from the source.

```sh
# 1. Install CRDs
kubectl apply -k config/crd

# 2. Deploy Controller with default configuration
kubectl apply -k config/default
```

You can enable optional components (Metrics, TLS, Webhook) by creating a `kustomization.yaml` that includes the relevant components from the `config/` directory. For reference on how these components can be combined, see the `deploy-with-metrics`, `deploy-with-tls`, `deploy-with-webhook`, and `deploy-full` targets in the projects [`Makefile`](https://github.com/kubernetes-sigs/node-readiness-controller/blob/main/Makefile).

### Option 4: Deploy as a Static Pod (Control Plane)

Running the controller as a **Static Pod** on control-plane nodes is useful for self-managed clusters (e.g., `kubeadm`) where you want the controller to be available alongside core components like the API server.

Refer to the `examples/static-pod/node-readiness-controller.yaml` for a detailed
example on deploying the controller as a static pod in a kind cluster.

#### Deployment Steps

1.  **Prepare the Manifest**:
    Refer the example manifest in
    `examples/static-pod/node-readiness-controller.yaml`. This manifest handles kubeconfig with a `initContainer` and necessary flags for leader election.

2.  **Deploy to Nodes**:
    Use Ansible / Terraform to copy the manifest to the `/etc/kubernetes/manifests/` directory on each control-plane node. The Kubelet will automatically detect and start the pod.

3.  **Install CRDs**:
    ```sh
    kubectl apply -k config/crd
    ```
    This is typically handled via a bootstrap script or post-install job in a `kubeadm` setup.

---
## Verification

After installation, verify that the controller is running successfully. 

> [!NOTE] 
> Replace `${NAMESPACE}` with the namespace where the controller is deployed (typically `nrr-system` for standard deployments, or `kube-system` for static pods).

1.  **Check Pod Status**:
    ```sh
    kubectl get pods -n ${NAMESPACE} -l component=node-readiness-controller
    ```
    You should see the controller pods in `Running` status.

2.  **Check Logs**:
    ```sh
    kubectl logs -n ${NAMESPACE} -l component=node-readiness-controller
    ```
    Look for "Starting EventSource" or "Starting Controller" messages indicating the manager is active.

3.  **Verify CRDs**:
    ```sh
    kubectl get crd nodereadinessrules.readiness.node.x-k8s.io
    ```

4. **Verify High Availability**:
    In an HA cluster, verify that one instance has acquired the leader lease:
    ```sh
    # The lease namespace should match the controller's namespace (configured via --leader-election-namespace)
    kubectl get lease -n ${NAMESPACE} ba65f13e.readiness.node.x-k8s.io
    ```

## Uninstallation

> [!IMPORTANT] 
> Follow this order to avoid "stuck" resources.

The controller uses a **finalizer** (`readiness.node.x-k8s.io/cleanup-taints`) on `NodeReadinessRule` resources to ensure taints are safely removed from nodes before a rule is deleted.

> [!CAUTION]
> You must delete all rule objects *before* deleting the controller.

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

    # OR if using Helm
    helm uninstall nrr-controller --namespace nrr-system

    # OR if using Static Pods
    # Remove the manifest from /etc/kubernetes/manifests/ on all control-plane nodes
    ```

    > [!CAUTION]
    > Rules declared through the chart's `nodeReadinessRules` value are part of the release, so `helm uninstall` deletes them and the controller in one operation. Helm does not wait for the finalizer to run, which is exactly the situation described in [Recovering from Stuck Resources](#recovering-from-stuck-resources). Delete the rules and let them finish terminating before uninstalling the release.

3.  **Uninstall CRDs** (Optional):
    ```sh
    kubectl delete -k config/crd
    ```
    Helm does not remove the CRD it installed from the chart's `crds/` directory, so delete it explicitly if you used the chart.

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
