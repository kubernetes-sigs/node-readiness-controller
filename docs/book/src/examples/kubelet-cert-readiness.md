# Kubelet Serving Certificate Readiness

This guide demonstrates how to use the Node Readiness Controller to prevent workloads from being scheduled on a node until kubelet has obtained its TLS serving certificate.

## The Problem

When a node joins a cluster, it is marked `Ready` once the container runtime and network plugin are operational. However, `kubectl exec` and `kubectl logs` require an additional step i.e. kubelet must have a valid TLS serving certificate.

These commands are routed through the Kubernetes API server, which opens a reverse TLS connection back to kubelet. Without the certificate, that connection fails. The certificate is requested separately via a `CertificateSigningRequest` and on some providers its approval can be delayed, long enough for pods to be scheduled and fail.

This surfaces in CI/CD environments where tools like **GitLab Runner** try to `exec` into pods on a new node and receive TLS errors, or where **Fluentbit** pulls metadata from kubelet before the certificate is in place.

## The Solution

We can use the Node Readiness Controller to enforce a kubelet certificate readiness guardrail:

1. **Taint** the node with `readiness.k8s.io/KubeletServingCertNotReady=pending:NoSchedule` when it joins, blocking workloads from scheduling.
2. **Monitor** the kubelet serving certificate using a **Node Problem Detector** custom plugin that checks for the certificate on the node's filesystem and reports the result as a node condition.
3. **Untaint** the node only after the certificate is confirmed to be present.

> [!NOTE]
> NRC does not perform the health check itself. It reacts to the state of
> `node.status.conditions` Any component that can write a condition to the
> Node object can act as the reporter. NPD is used here because it is a
> natural fit for node-local checks such as inspecting a file path, but it
> is not the only option. See [Condition Reporting](../user-guide/concepts.md#readiness-condition-reporting)
> for alternatives.

## Step-by-Step Guide

> [!NOTE]
> All manifests referenced in this guide are available in the
> [`examples/kubelet-cert-readiness/manifests`](https://github.com/kubernetes-sigs/node-readiness-controller/tree/main/examples/kubelet-cert-readiness/manifests)
> directory.

### Prerequisites

**1. Node Readiness Controller:**

Before starting, ensure the Node Readiness Controller is deployed. See the [Installation Guide](../user-guide/installation.md) for details.

**2. Kubernetes Cluster with Worker Nodes:**

This example requires at least one worker node with the startup taint. 

For kind clusters, save the provided configuration in [`examples/kubelet-cert-readiness/kind-config.yaml`](https://github.com/kubernetes-sigs/node-readiness-controller/blob/main/examples/security-agent-readiness/kind-cluster-config.yaml) to a file, then create the cluster using the following command:

```sh
kind create cluster --config <your-kind-config-file.yaml>
```

This creates a cluster with:
- 1 control-plane node
- 1 worker node pre-tainted with `readiness.k8s.io/KubeletServingCertNotReady=pending:NoSchedule`
-  `serverTLSBootstrap: true` enabled on both nodes. With this setting, kubelet requests its serving certificate via a CSR instead of generating a self-signed one, which creates the exact timing gap this example addresses.

### 1. Deploy the NPD Custom Plugin

We create a ConfigMap containing the check script and the NPD plugin configuration, then deploy NPD with these mounted into the container.

**Plugin configuration:**

The plugin uses a `permanent` rule, which creates and maintains a real entry in `node.status.conditions`. This is required for NRC to react to the condition.

```yaml
# npd-configmaps.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: npd-kubelet-cert-config
  namespace: kube-system
data:
  kubelet-serving-cert-monitor.json: |
    {
      "plugin": "custom",
      "pluginConfig": {
        "invoke_interval": "10s",
        "timeout": "5s",
        "max_output_length": 80,
        "concurrency": 1
      },
      "source": "kubelet-serving-cert-monitor",
      "conditions": [
        {
          "type": "KubeletServingCertNotReady",
          "reason": "KubeletServingCertPresent",
          "message": "kubelet serving certificate is present"
        }
      ],
      "rules": [
        {
          "type": "permanent",
          "condition": "KubeletServingCertNotReady",
          "reason": "KubeletServingCertMissing",
          "path": "/config/plugin/check-kubelet-serving-cert.sh",
          "timeout": "5s"
        }
      ]
    }
  check-kubelet-serving-cert.sh: |
    #!/bin/sh
    CERT_PATH="/var/lib/kubelet/pki/kubelet-server-current.pem"
    if [ -f "$CERT_PATH" ]; then
      echo "kubelet serving certificate present at $CERT_PATH"
      exit 0
    else
      echo "kubelet serving certificate not yet present at $CERT_PATH"
      exit 1
    fi
```

**NPD DaemonSet:**

The DaemonSet mounts the check script and plugin config as volumes, and mounts the node's `/var/lib/kubelet/pki` directory read-only so the script can inspect it.

```yaml
# npd-daemonset.yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-problem-detector
  namespace: kube-system
  labels:
    app: node-problem-detector
spec:
  selector:
    matchLabels:
      app: node-problem-detector
  template:
    metadata:
      labels:
        app: node-problem-detector
    spec:
      serviceAccountName: node-problem-detector
      tolerations:
        - key: "readiness.k8s.io/KubeletServingCertNotReady"
          operator: "Exists"
          effect: "NoSchedule"
      containers:
        - name: node-problem-detector
          image: registry.k8s.io/node-problem-detector/node-problem-detector:v0.8.20
          command:
            - /node-problem-detector
            - --logtostderr
            - --config.custom-plugin-monitor=/config/plugin-monitor/kubelet-serving-cert-monitor.json
          env:
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
          volumeMounts:
            - name: plugin-monitor-config
              mountPath: /config/plugin-monitor
            - name: plugin-script
              mountPath: /config/plugin
            - name: kubelet-pki
              mountPath: /var/lib/kubelet/pki
              readOnly: true
      volumes:
        - name: plugin-monitor-config
          configMap:
            name: npd-kubelet-cert-config
            items:
              - key: kubelet-serving-cert-monitor.json
                path: kubelet-serving-cert-monitor.json
        - name: plugin-script
          configMap:
            name: npd-kubelet-cert-config
            defaultMode: 0755
            items:
              - key: check-kubelet-serving-cert.sh
                path: check-kubelet-serving-cert.sh
        - name: kubelet-pki
          hostPath:
            path: /var/lib/kubelet/pki
```

See the complete NPD manifests including RBAC in [`examples/kubelet-cert-readiness/manifests/`](https://github.com/kubernetes-sigs/node-readiness-controller/tree/main/examples/kubelet-cert-readiness/manifests).

### 2. Create the Node Readiness Rule

Define a `NodeReadinessRule` that instructs NRC to remove the startup taint once `KubeletServingCertNotReady` becomes `False`.

```yaml
# nrc-rule.yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: kubelet-serving-cert-readiness
spec:
  conditions:
    - type: "KubeletServingCertNotReady"
      requiredStatus: "False"

  taint:
    key: "readiness.k8s.io/KubeletServingCertNotReady"
    effect: "NoSchedule"
    value: "pending"

  enforcementMode: "bootstrap-only"
  nodeSelector:
    matchExpressions:
      - key: node-role.kubernetes.io/control-plane
        operator: DoesNotExist
```

## Deploy the Example

```sh
kubectl apply -f examples/kubelet-cert-readiness/manifests/
```

## Verification

1. **Check the startup taint is applied:**

   ```sh
   kubectl get nodes -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints
   ```

   Worker node should show `readiness.k8s.io/KubeletServingCertNotReady=pending:NoSchedule`

2. **Check the node condition:**

   ```sh
   kubectl get node <node-name> \
    -o jsonpath='{.status.conditions[?(@.type=="KubeletServingCertNotReady")]}' | jq .
   ```

   Initially `KubeletServingCertNotReady=True`. It means certificate is missing, taint remains.

3. **Approve the worker's pending CSR to simulate the certificate being issued:**

    ```sh
    kubectl get csr
    # find the entry with SIGNERNAME kubernetes.io/kubelet-serving and REQUESTOR system:node:<worker-name>
    kubectl certificate approve <worker-csr-name>
    ```

4. **Check the condition again:**

    ```sh
    kubectl get node <node-name> \
    -o jsonpath='{.status.conditions[?(@.type=="KubeletServingCertNotReady")]}' | jq .
    ```

    `KubeletServingCertNotReady=False`. It means certificate is present, NRC removes the taint.

5. **Check taint removal:**

   ```sh
   kubectl get node <node-name> -o jsonpath='{.spec.taints}'
   ```
    As soon as `KubeletServingCertNotReady` becomes `False`, NRC removes the startup taint and the node becomes available for workloads.

4. **Confirm `kubectl exec` works:**

   ```sh
   kubectl run test-pod --image=busybox --restart=Never -- sleep 3600
   
   kubectl exec test-pod -- echo "exec works"
   ```

   This should succeed only after the taint has been removed.