# PodsAPI Smoketest (Verify podsapi working for NRD client)

A standalone gRPC client that talks to the Kubelet PodsAPI (KEP-4188, alpha in
Kubernetes 1.36, feature gate `PodsAPI`). Tested against a `kindest/node:v1.36.1` cluster.

It does **not** build inside this repo's module (pinned to k8s 1.34; the
`k8s.io/kubelet/pkg/apis/pods/v1alpha1` proto requires k8s ≥ 1.36). Build it against
a Kubernetes ≥ 1.36 source tree or module instead:

```bash
# from a kubernetes checkout at >= v1.36 (which provides the proto via go.work):
cd $NRC_HOME
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/podsapiprobe ./hack/podsapi-smoketest

# run it inside a node that has the socket (for example in a kind cluster, node name as kind-worker):
docker cp /tmp/podsapiprobe <node>:/usr/local/bin/podsapiprobe
docker exec <node> /usr/local/bin/podsapiprobe
```

Create a kind cluster with the podsapi feature-gate enabled:

```bash
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
- role: worker
YAML
```