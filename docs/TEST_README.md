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

-   [Docker](https://docs.docker.com/get-docker/) or [Podman](https://podman.io/getting-started/installation)
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

**Using Docker:**
```bash
# Build the image (uses defaults: IMG_PREFIX=controller IMG_TAG=latest)
make docker-build

# Load the image into the kind cluster (uses default: KIND_CLUSTER=nrr-test)
make kind-load

# Verify the image is loaded
docker exec -it nrr-test-control-plane crictl images | grep controller
```

**Using Podman:**
```bash
# Build the image (uses defaults: IMG_PREFIX=controller IMG_TAG=latest)
make podman-build

# Load the image into the kind cluster (uses default: KIND_CLUSTER=nrr-test)
make kind-load CONTAINER_TOOL=podman

# Verify the image is loaded
podman exec -it nrr-test-control-plane crictl images | grep controller
```

### Step 3: Controller Deployment

Deploy the controller to the cluster.

**Using Docker:**
```bash
make deploy IMG_PREFIX=controller IMG_TAG=latest
```

**Using Podman:**
```bash
make deploy IMG_PREFIX=localhost/controller IMG_TAG=latest
```

**Optional: Deploy with Metrics Enabled (for Step 10):**

If you want to test metrics (see Step 10), deploy with metrics enabled:

Using Docker:
```bash
make deploy-with-metrics IMG_PREFIX=controller IMG_TAG=latest
```

Using Podman:
```bash
make deploy-with-metrics IMG_PREFIX=localhost/controller IMG_TAG=latest
```

Alternatively, you can use:
```bash
make deploy ENABLE_METRICS=true IMG_PREFIX=controller IMG_TAG=latest
```

> **Note:** Metrics are exposed on HTTP port 8080. For production with TLS-secured metrics, use `make deploy-with-metrics-and-tls` (requires cert-manager).

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

This script installs calico and deploy CNI readiness reporter DaemonSet that updates node condition based on CNI readiness.

```bash
chmod +x examples/cni-readiness/apply-calico.sh
examples/cni-readiness/apply-calico.sh
```

### Step 8: Monitor and Verify Final State

1.  **Check for the new node condition on the application worker node:**
    ```bash
    kubectl get node nrr-test-worker2 -o json | jq '.status.conditions[] | select(.type=="projectcalico.org/CalicoReady")'

2. **Look for 'projectcalico.org/CalicoReady   True'**
    ```bash
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

### Step 10: Testing Metrics

The controller exposes Prometheus metrics on port 8080 at the `/metrics` endpoint. This section demonstrates how to access and verify the metrics.

#### Option 1: Port Forward to Metrics Service

1. **Port forward to the controller metrics endpoint:**
   ```bash
   kubectl port-forward -n nrr-system svc/nrr-metrics-service 8080:8080
   ```

2. **Access metrics in your browser or via curl:**
   ```bash
   # View all metrics
   curl http://localhost:8080/metrics

   # Filter for node readiness specific metrics
   curl http://localhost:8080/metrics | grep node_readiness
   ```

#### Option 2: Direct Pod Access

1. **Get the controller pod name:**
   ```bash
   CONTROLLER_POD=$(kubectl get pods -n nrr-system -l control-plane=controller-manager -o jsonpath='{.items[0].metadata.name}')
   echo $CONTROLLER_POD
   ```

2. **Port forward directly to the pod:**
   ```bash
   kubectl port-forward -n nrr-system $CONTROLLER_POD 8080:8080
   ```

3. **Query metrics:**
   ```bash
   curl http://localhost:8080/metrics | grep node_readiness
   ```

#### Key Metrics to Monitor

After running the test scenario, you should see the following metrics:

1. **Rule Management:**
   ```bash
   # Number of active rules
   curl -s http://localhost:8080/metrics | grep "node_readiness_rules_total"
   ```

2. **Taint Operations:**
   ```bash
   # Total taint add/remove operations by rule
   curl -s http://localhost:8080/metrics | grep "node_readiness_taint_operations_total"
   ```

3. **Node State Distribution:**
   ```bash
   # Number of nodes in each state (ready/not_ready/bootstrapping)
   curl -s http://localhost:8080/metrics | grep "node_readiness_nodes_by_state"
   ```

4. **Bootstrap Metrics:**
   ```bash
   # Bootstrap completion count
   curl -s http://localhost:8080/metrics | grep "node_readiness_bootstrap_completed_total"
   
   # Bootstrap duration histogram
   curl -s http://localhost:8080/metrics | grep "node_readiness_bootstrap_duration_seconds"
   ```

5. **Performance Metrics:**
   ```bash
   # Rule evaluation duration
   curl -s http://localhost:8080/metrics | grep "node_readiness_evaluation_duration_seconds"
   
   # Reconciliation latency
   curl -s http://localhost:8080/metrics | grep "node_readiness_reconciliation_latency_seconds"
   ```

6. **Failure Tracking:**
   ```bash
   # Operational failures by rule and reason
   curl -s http://localhost:8080/metrics | grep "node_readiness_failures_total"
   
   # Condition evaluation failures
   curl -s http://localhost:8080/metrics | grep "node_readiness_condition_failures_total"
   ```

7. **Rule Reconciliation Status:**
   ```bash
   # Last reconciliation timestamp for each rule
   curl -s http://localhost:8080/metrics | grep "node_readiness_rule_last_reconciliation_timestamp_seconds"
   ```

#### Example: Verify Metrics After Test

After completing Steps 1-9, verify the metrics reflect the test scenario:

```bash
# Should show 1 rule (network-readiness-rule)
curl -s http://localhost:8080/metrics | grep 'node_readiness_rules_total'

# Should show taint removal operations for worker2, worker3, worker4
curl -s http://localhost:8080/metrics | grep 'node_readiness_taint_operations_total{.*operation="remove"}'

# Should show nodes in ready state
curl -s http://localhost:8080/metrics | grep 'node_readiness_nodes_by_state{.*state="ready"}'

# Should show bootstrap completions (if using bootstrap-only mode)
curl -s http://localhost:8080/metrics | grep 'node_readiness_bootstrap_completed_total'
```

### Step 11: Cleanup

```bash
kind delete cluster --name nrr-test
```
