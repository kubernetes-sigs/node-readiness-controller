# Node Readiness Daemon (NRD) — Prototype

A node-local daemon that watches pod health and publishes
`Node.status.conditions` consumed by the Node Readiness Controller.

> **Status: Prototype.** The full daemon core (config loader, pod cache,
> evaluators, debounce, node-impersonated writer) is implemented and
> tested. The Kubelet `PodsAPI` (KEP-4188) is Beta from 1.37. This prototype is verified end-to-end against
> `kindest/node:v1.36.1`** — see [Live test](#live-test-against-a-real-136-kubelet).
> The `--source=fake` is available for testing. Requires controller-runtime v0.24.

## Layout

| Code Path | Description |
|---|---|
| `internal/daemon/podsource.go` | `PodSource` interface + `FakePodSource`. The real WatchPods client plugs in here. |
| `internal/daemon/cache.go` | UID-keyed pod cache with `INITIAL_SYNC_COMPLETE` gating. |
| `internal/daemon/config.go` | load config from `conditions.d/`. handles config validation, hot-reload, kubelet-restart, dedup. |
| `internal/daemon/evaluator.go` | Health logic: `PodReady` + fail-closed `minPods`. |
| `internal/daemon/debounce.go` | Hysteresis support with asymmetric healthy/unhealthy thresholds. |
| `internal/daemon/writer.go` | `ConditionWriter`: node-scoped `UpdateStatus`. |
| `internal/daemon/daemon.go` | Orchestrator: pod-stream from kubelet to cache and evaluate readiness based on configuration. |
| `cmd/node-readiness-daemon/` | Entrypoint supports `--source=kubelet`  and `--source=fake` for testing|


## Tests

```bash
go test ./internal/daemon/...
```

Tests added to cover:
* config handling: validation/reload/dedup
* fail-closed `minPods` evaluator
* debounce, cache sync-gating, and
* condition writer handling.

## E2E
### pre-1.36 cluster

The fake source scripts a critical pod that starts not-ready then becomes ready;
`--dry-run` logs the conditions instead of writing them:

```bash
go run ./cmd/node-readiness-daemon \
  --source=fake \
  --node-name=demo-node \
  --config-dir=examples/node-readiness-daemon/conditions.d \
  --dry-run \
  --reconcile-interval=1s
```

The sample config uses a `healthyThreshold: 30s`, so the
`cni.example.io/NetworkReady` condition stays absent (effective status: Unknown) for the
first 30s and then resolves to `True`. This delay is the debounce working as designed, not a hang.

#### Write node conditions to a cluster and NRC reacts to readiness update

Drop `--dry-run` and point at a kubeconfig (with fake pod source);
watch NRC react to the published condition:

```bash
go run ./cmd/node-readiness-daemon \
  --source=fake --node-name=<a-sample-node> \
  --config-dir=examples/node-readiness-daemon/conditions.d \
  --kubeconfig=$HOME/.kube/config
```

Add `--impersonate-node` flag for the writer to use impersonated
`system:node:<node-name>` and confined updates with NodeRestriction to that node.


### Live test against a 1.36 kubelet

Verified end-to-end on `kindest/node:v1.36.1`.

To Reproduce:

```bash
# 1. Cluster with the kubelet PodsAPI gate enabled.
kind create cluster --image kindest/node:v1.36.1 --config - <<'YAML'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        feature-gates: "PodsAPI=true"
YAML

# 2. Build the daemon and copy the daemon + sample config to the worker node.
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/nrd ./cmd/node-readiness-daemon
docker cp /tmp/nrd <node>:/usr/local/bin/nrd
# conditions.d/*.yaml written into /etc/nrd/conditions.d on the node

# 3. Use source as kubelet inside the node (reads the pod-api grpc socket and writes node conditions).
docker exec -d <node> /usr/local/bin/nrd \
  --source=kubelet \
  --kubeconfig=/etc/kubernetes/admin.conf \
  --node-name=<node> \
  --config-dir=/etc/nrd/conditions.d \
  --reconcile-interval=2s

# 4. Observe real conditions derived from live pod state:
kubectl get node <node> -o json | jq '.status.conditions[] | select(.type|contains("/"))'
```

Example:
The NRD daemon monitoring the readiness state for coredns pods. This is driven by the live WatchPods ADDED/MODIFIED/DELETED stream.
When pods meet minPods=1 criteria, `dns.example.io/CoreDNSReady` condition will set to True; Trigger a fail condition by deleting a matching pod,
the condition will flip to `False`.

The NRD daemon is best deployed to run as a systemd daemon on the node. For testing in your cluster, deploy NRD as a privileged DaemonSet with the socket path mounted as `hostPath`
(`/var/lib/kubelet/pods-api/pods-api.sock`) and `--impersonate-node` so writes go out as `system:node:<node>` (NodeRestriction-scoped). A reference probe in
[`hack/podsapi-probe`](../../hack/podsapi-probe) can be used as a minimal smoke-test.
