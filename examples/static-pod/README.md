# Running the Node Readiness Controller as a Static Pod

This example demonstrates how to run the Node Readiness Controller as a **Static Pod** on the control-plane nodes. This is useful for self-managed clusters (e.g., `kubeadm`) where you want the controller to be available alongside other core components like the API server.

## Key Features of this Setup

1.  **Non-Root Security:** The controller container runs as a non-root user (`UID 65532`), following security best practices.
2.  **Permissions Handling:** Since `/etc/kubernetes/admin.conf` is typically restricted to `root:root (0600)`, an `initContainer` is used to copy the kubeconfig to a shared `emptyDir` volume and set readable permissions (`0644`) for the non-root manager process.
3.  **High Availability:** The example configuration is compatible with multi-master HA setups, using leader election to ensure only one instance is active.

## Files

- `node-readiness-controller.yaml`: The Static Pod manifest.
- `kind-static-pod.yaml`: A Kind cluster configuration that mounts the manifest into control-plane nodes for testing.

## Local Testing with Kind

### 1. Build the Image
```bash
make docker-build IMG_PREFIX=controller IMG_TAG=latest
```

### 2. Create the HA Cluster
```bash
kind create cluster --config examples/static-pod/kind-static-pod.yaml --name nrr-static-test
```

### 3. Load the Image
```bash
kind load docker-image controller:latest --name nrr-static-test
```

### 4. Verify Pods
Static pods are managed by the Kubelet on each node. They will appear in the `kube-system` namespace:
```bash
kubectl get pods -n kube-system -l component=node-readiness-controller
```

### 5. Check Leader Election
```bash
kubectl get lease -n kube-system ba65f13e.readiness.node.x-k8s.io
```

## Production Deployment (e.g., kubeadm)

1.  Copy `node-readiness-controller.yaml` to the `/etc/kubernetes/manifests/` directory on each control-plane node.
2.  Ensure the image specified in the manifest is available on the host (or in a registry the host can access).
3.  The Kubelet will automatically detect and start the pod.
