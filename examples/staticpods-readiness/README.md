# Node Readiness Examples (Static Pods)

This example demonstrates how NRC can be used alongside [NPD (Node Problem Detector)](https://github.com/kubernetes/node-problem-detector) to make sure that all static pods are mirrored in the API server to avoid over-committing issues. See [#115325](https://github.com/kubernetes/kubernetes/issues/115325), [#47264](https://github.com/kubernetes/website/issues/47264), and [#126870](https://github.com/kubernetes/kubernetes/pull/126870) for reference.

## Deployment Steps

1. Deploy a testing cluster. For this example we use Kind with a pre-tainted worker node and a mounted static pod manifest:

   ```bash
   kind create cluster --config examples/staticpods-readiness/kind-cluster-config.yaml
   ```

2. Install NRC:

   ```bash
   VERSION=v0.2.0
   kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/${VERSION}/crds.yaml
   kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/${VERSION}/install.yaml
   ```

3. Deploy NPD as a DaemonSet with the static pods readiness configuration:

   ```bash
   ./examples/staticpods-readiness/setup-staticpods-readiness.sh
   ```

   This deploys NPD with:
   - Init container that downloads `kubectl`, `yq`, and `jq`
   - Custom plugin that checks if static pods are mirrored
   - RBAC permissions to read pods and nodes

4. Apply the node readiness rule:
   ```bash
      kubectl apply -f examples/staticpods-readiness/staticpods-readiness-rule.yaml
   ```

## How It Works

The NPD problem daemon script runs every 30s:
- Finds manifests in `/etc/kubernetes/manifests`
- Parses the manifests and checks if mirror pod `{pod-name}-{node-name}` exists
- Sets `StaticPodsMissing=False` if all pods are mirrored