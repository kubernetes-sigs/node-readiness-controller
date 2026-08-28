# Static Pod Deployment

This example demonstrates deploying Node Readiness Controller (NRC) itself as a static pod on control-plane nodes.

## Why deploy NRC as a static pod?

In most clusters, NRC is deployed as a standard Kubernetes `Deployment`. For clusters that require NRC to be available during early node bootstrap, before the API server is fully operational or that need the controller to be distributed across control-plane nodes for high availability, deploying NRC as a static pod on each control-plane node is an alternative.

## How it works

1. The NRC pod manifest (`node-readiness-controller.yaml`) is placed in `/etc/kubernetes/manifests/` on each control-plane node.
2. Kubelet on each control-plane node starts NRC directly from the manifest, without involving the scheduler.
3. An init container copies the node's kubeconfig to a shared volume and sets its permissions, making it accessible to the non-root NRC process.
4. NRC runs with `hostNetwork: true` and `priorityClassName: system-node-critical`, consistent with other control-plane static pods.

## Files

| File | Description |
|---|---|
| `node-readiness-controller.yaml` | NRC pod manifest for static pod deployment. Place this file in `/etc/kubernetes/manifests/` on each control-plane node. |
| `kind-static-pod.yaml` | Kind cluster configuration for local testing. Creates a three-control-plane cluster with `node-readiness-controller.yaml` pre-mounted at the static pod path on each control-plane node, and two worker nodes with a `readiness.k8s.io/NetworkReady=pending:NoSchedule` startup taint. |

## Local testing

```bash
kind create cluster --config kind-static-pod.yaml
```

After the cluster starts, verify NRC is running as a static pod on the control-plane nodes:

```bash
kubectl get pods -n kube-system | grep node-readiness-controller
```

You should see one mirror pod per control-plane node, each with a `-<nodename>` suffix.