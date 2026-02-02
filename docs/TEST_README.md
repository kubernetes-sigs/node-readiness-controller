# Node Readiness Gates E2E Test Guide (Kind)

This guide details how to run an end-to-end test for the Node Readiness Rules (NRR) controller using a local Kind cluster.

The test demonstrates a realistic, production-aligned scenario where critical addons run on a dedicated platform node pool, and the NRR controller manages a network readiness taint on a separate application worker node.

### Test Topology

The test uses a 3-node Kind cluster:
1.  **`nrr-test-control-plane`**: The Kubernetes control plane. The NRR controller will run here unless specifically configured.
2.  **`nrr-test-worker` (Platform Node)**: A dedicated node for running cluster-critical addons. It is labeled `reserved-for=platform` and has a corresponding taint to repel normal application workloads. Cert-manager will run here.
3.  **`nrr-test-worker2` (Application Node)**: A standard worker node that starts with a `readiness.k8s.io/NetworkReady=pending:NoSchedule` taint, simulating a node that is not yet ready for application traffic.

## Running the Test

### Prerequisites

-   [Docker](https://docs.docker.com/get-docker/)
-   [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
-   [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
-   [Go](https://golang.org/doc/install)

### Step 1: Create the Kind Cluster

The provided Kind configuration creates the 3-node topology with the necessary labels and taints.

```bash
kind create cluster --config config/testing/kind/kind-3node-config.yaml
```

Install CRDs

```bash
make install
```

### Step 2: Build and Load the Controller Image

Build the controller image and load it into the Kind cluster nodes.

```bash
# Build the image
make docker-build IMG_PREFIX=controller IMG_TAG=latest

# Load the image into the kind cluster
kind load docker-image controller:latest --name nrr-test
```

### Step 3: Controller Deployment

Deploy the controller image to nrr-test-worker
```bash
make deploy IMG_PREFIX=controller IMG_TAG=latest
```

Verify the controller is running on the control plane node (`nrr-test-control-plane`):
```bash
kubectl get pods -n nrr-system -o wide
```

### Step 4: Deploy the Readiness Rule

Apply the network readiness rule. This will be validated by the webhook.

```bash
kubectl apply -f examples/cni-readiness/network-readiness-rule.yaml
```

### Step 6: Verify Initial State

Check that the application worker node (`nrr-test-worker2`) has the `NetworkReady` taint.

```bash
# The output should include 'readiness.k8s.io/NetworkReady'
kubectl get node nrr-test-worker2 -o jsonpath='Taints:{"\n"}{range .spec.taints[*]}{.key}{"\n"}{end}'
```

### Step 7: Deploy Calico CNI with Readiness Reporter

This script injects the readiness sidecar into the Calico deployment.

```bash
chmod +x examples/cni-readiness/apply-calico.sh
examples/cni-readiness/apply-calico.sh
```

### Step 8: Monitor and Verify Final State

1.  **Check for the new node condition on the application node:**
    ```bash
    kubectl get node $NODE -o json | jq '.status.conditions[] | select(.type=="projectcalico.org/CalicoReady")'
# Look for 'projectcalico.org/CalicoReady   True'
    kubectl get node nrr-test-worker2 -o jsonpath='Conditions:{"\n"}{range .status.conditions[*]}{.type}{"\t"}{.status}{"\n"}{end}'
    ```

2.  **Verify the taint has been removed from the application node:**
    ```bash
    # The output should NO LONGER include 'readiness.k8s.io/NetworkReady'
    kubectl get node nrr-test-worker2 -o jsonpath='Taints:{"\n"}{range .spec.taints[*]}{.key}{"\n"}{end}'
    ```

### Step 9: Autoscaling Simulation Test

This section tests how the controller handles new nodes being added to the cluster, simulating an autoscaler.

1.  **Scale up the worker nodes:**
    ```bash
    # Add 2 new worker nodes (for a total of 4 workers)
    hack/test-workloads/kindscaler.sh nrr-test 2
    ```

2.  **Verify new nodes are tainted:**
    ```bash
    # Check the taints on the new nodes
    kubectl get node nrr-test-worker3 -o jsonpath='Taints:{"\n"}{range .spec.taints[*]}{.key}{"\n"}{end}'
    kubectl get node nrr-test-worker4 -o jsonpath='Taints:{"\n"}{range .spec.taints[*]}{.key}{"\n"}{end}'
    ```

3.  **Verify taints are removed after Calico is ready:**
    It may take a minute for the Calico pods to be scheduled and run on the new nodes.
    ```bash
    # Wait and verify the taints are removed from the new nodes
    sleep 60
    kubectl get node nrr-test-worker3 -o jsonpath='Taints:{"\n"}{range .spec.taints[*]}{.key}{"\n"}{end}'
    kubectl get node nrr-test-worker4 -o jsonpath='Taints:{"\n"}{range .spec.taints[*]}{.key}{"\n"}{end}'
    ```

### Step 10: Cleanup

```bash
kind delete cluster --name nrr-test
```
