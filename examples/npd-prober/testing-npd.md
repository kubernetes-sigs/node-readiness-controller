# Testing NPD Prober on a Kind Cluster

## How `npd-config.json` maps to `node-readiness-rule.yaml`

The connection point is the **NodeCondition type** — `ServiceReadiness`.

**On the Node-Problem-Detector (NPD) side (`npd-config.json`):**
- The `conditions` array declares a condition with `"type": "ServiceReadiness"` — this is the NodeCondition NPD will manage on each node.
- The `rules` array has a permanent rule with `"condition": "ServiceReadiness"` — when the prober exits with `1` (NonOK), NPD sets `ServiceReadiness=True` (problem present) with reason `ServiceNotReady`. When it exits with `0` (OK), NPD sets `ServiceReadiness=False` (no problem) with reason `ServiceIsReady`.

**On the Node-Readiness-Controller (NRC) side (`node-readiness-rule.yaml`):**
- `spec.conditions[0].type: "ServiceReadiness"` — watches the exact same condition NPD sets.
- `spec.conditions[0].requiredStatus: "False"` — the taint is removed when this condition is `False` (no problem).
- `spec.taint` — defines what taint to manage based on that condition state.

> **Important:** NPD conditions represent **problems**, not health. Exit code 0 (OK) sets the
> condition to `False` (problem absent), while exit code 1 (NonOK) sets it to `True` (problem
> present). This is why `requiredStatus` is `"False"` — the node is ready when the problem
> condition is not active.

```
npd-config.json                        node-readiness-rule.yaml
─────────────────                      ────────────────────────
conditions[].type: "ServiceReadiness"  ──►  conditions[].type: "ServiceReadiness"
                                            conditions[].requiredStatus: "False"

rules[].reason: "ServiceNotReady"      (exit 1 → NPD sets condition True
                                        → NRC sees True ≠ False → taint applied)

conditions[].reason: "ServiceIsReady"  (exit 0 → NPD sets condition False
                                        → NRC sees False = False → taint removed)
```

## 1. Build the prober binary (Linux)

```bash
git clone https://github.com/kubernetes-sigs/node-readiness-controller.git
cd node-readiness-controller
# Build the npd-prober 
GOOS=linux GOARCH=amd64 go build -o npd-prober ./examples/npd-prober/
```

## 2. Create a Kind cluster with a worker node

The `NodeReadinessRule` targets non-control-plane nodes (`node-role.kubernetes.io/control-plane DoesNotExist`),
so we need a worker node. Create a Kind config:

```bash
cat <<'EOF' > /tmp/kind-npd-prober.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
EOF

kind create cluster --name npd-prober-test --config /tmp/kind-npd-prober.yaml
```

## 3. Deploy a sample workload to probe

Create a simple health endpoint pod scheduled on the worker node. This application will
acknowledge the probes emitted by the npd-prober on the node:

```bash
kubectl run healthz-server --image=registry.k8s.io/e2e-test-images/agnhost:2.39 \
  --command -- /agnhost serve-hostname --port 8080
kubectl expose pod healthz-server --port=8080
```

> **Note:** The pod will land on the worker node by default since control-plane nodes
> have a `NoSchedule` taint. Verify with `kubectl get pod healthz-server -o wide`.

## 4. Install NPD with the custom plugin

```bash
# Pull the NPD image and load it into Kind:
docker pull registry.k8s.io/node-problem-detector/node-problem-detector:v1.35.2
kind load docker-image registry.k8s.io/node-problem-detector/node-problem-detector:v1.35.2 --name npd-prober-test
```

Create a ConfigMap with the prober config and mount it + the binary into NPD. Save this as `npd-deploy.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: npd-prober-config
  namespace: kube-system
data:
  readiness-prober.json: |
    {
      "plugin": "custom",
      "pluginConfig": {
        "invoke_interval": "10s",
        "timeout": "5s",
        "max_output_length": 80,
        "concurrency": 1
      },
      "source": "readiness-prober-custom-plugin-monitor",
      "conditions": [
        {
          "type": "ServiceReadiness",
          "reason": "ServiceIsReady",
          "message": "service readiness probe is passing"
        }
      ],
      "rules": [
        {
          "type": "permanent",
          "condition": "ServiceReadiness",
          "reason": "ServiceNotReady",
          "path": "/custom-plugins/npd-prober",
          "args": ["--probe-type=http", "--http-url=http://healthz-server.default.svc.cluster.local:8080", "--timeout=5s"],
          "timeout": "5s"
        }
      ]
    }
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-problem-detector
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: node-problem-detector
  template:
    metadata:
      labels:
        app: node-problem-detector
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      serviceAccountName: node-problem-detector
      containers:
        - name: npd
          image: registry.k8s.io/node-problem-detector/node-problem-detector:v1.35.2
          command: ["/node-problem-detector"]
          args:
            - "--logtostderr"
            - "--custom-plugin-monitors=/config/readiness-prober.json"
          volumeMounts:
            - name: config
              mountPath: /config
            - name: custom-plugins
              mountPath: /custom-plugins
          securityContext:
            privileged: true
          env:
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
      volumes:
        - name: config
          configMap:
            name: npd-prober-config
        - name: custom-plugins
          hostPath:
            path: /opt/npd-prober
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: node-problem-detector
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: node-problem-detector
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:node-problem-detector
subjects:
  - kind: ServiceAccount
    name: node-problem-detector
    namespace: kube-system
```

Copy the prober binary into all Kind nodes, then deploy NPD:

```bash
# Copy binary into each Kind node
for NODE in $(kind get nodes --name npd-prober-test); do
  docker exec "${NODE}" mkdir -p /opt/npd-prober
  docker cp npd-prober "${NODE}:/opt/npd-prober/npd-prober"
  docker exec "${NODE}" chmod +x /opt/npd-prober/npd-prober
done

# Deploy NPD
kubectl apply -f npd-deploy.yaml
```

## 5. Verify NPD sets the condition

```bash
# Wait a few seconds, then check:
kubectl get node -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{range .status.conditions[*]}  {.type}={.status}{"\n"}{end}{end}'
```

You should see `ServiceReadiness=False` (probe healthy, no problem) or `ServiceReadiness=True` (probe failing, problem active) in the list.

## 6. Install the Node Readiness Controller and apply the rule

```bash
cd node-readiness-controller
make install
make run

```

On a different terminal apply the NodeReadinessRule config.
```bash
kubectl apply -f examples/npd-prober/node-readiness-rule.yaml
```

## 7. Verify end-to-end

```bash
# Check taints — should have no ServiceReady taint if probe is healthy:
kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.taints}{"\n"}{end}'

# Check the rule status:
kubectl get nrr service-readiness-rule -o yaml
```

## 8. Simulate failure and recovery

Misconfigure the service port so the probe can no longer reach the health endpoint.
The pod keeps running — only the service routing is broken:

```bash
# Point the service at a wrong targetPort (pod listens on 8080, not 9999)
kubectl patch svc healthz-server --type='json' \
  -p='[{"op":"replace","path":"/spec/ports/0/targetPort","value":9999}]'
```

After ~10s (NPD invoke interval), the condition should flip to `True` (problem active) and the taint `readiness.k8s.io/ServiceReady:pending:NoSchedule`should appear:

```bash
kubectl get nodes -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints
```

Now fix the service port to verify recovery:

```bash
# Restore the correct targetPort
kubectl patch svc healthz-server --type='json' \
  -p='[{"op":"replace","path":"/spec/ports/0/targetPort","value":8080}]'
```

After ~10s, the probe should succeed again, the condition flips to `False`, and the taint is removed:

```bash
kubectl get nodes -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints
```

## 9. Cleanup

```bash
kind delete cluster --name npd-prober-test
```
