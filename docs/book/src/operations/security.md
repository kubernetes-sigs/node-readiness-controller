# Security

The Node Readiness Controller relies on external **reporters**, lightweight components running on each node, to publish readiness information as Node conditions. Because these reporters must patch Node status objects, the RBAC they require needs careful attention.

## The Threat Model

Reporters typically run as DaemonSet pods or sidecars on the nodes they monitor.
They use the Kubernetes API to update `nodes/status` with condition data that the controller consumes.

With **broad RBAC** (the default in Kubernetes < v1.35), a reporter's ServiceAccount is granted `patch` and `update` on all `nodes/status` resources cluster-wide. This means that if a single node is compromised, an attacker can:

- Mark **other** nodes as ready or not-ready, influencing scheduling decisions across the cluster.
- Inject false conditions to bypass readiness gates on nodes they do not control.

This violates the principle of least privilege: a reporter should only be able to modify the status of the node it runs on.

## Constrained Impersonation (KEP-5284)

[Constrained Impersonation](https://github.com/kubernetes/enhancements/issues/5284) (KEP-5284) introduces authorization rules that restrict a ServiceAccount to impersonating only the Node identity associated with the pod's bound service account token, and to performing only specific actions during that impersonation.

### How It Works

Two ClusterRoles are used together:

1. **Impersonation role** — grants the reporter the ability to impersonate the identity of its own Node and nothing else.

   ```yaml
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: node-readiness-impersonator
   rules:
   - apiGroups: ["authentication.k8s.io"]
     resources: ["nodes"]
     verbs: ["impersonate:associated-node"]
   ```

   The `impersonate:associated-node` verb tells the API server to validate that the pod's bound service account token references the same node (via the `authentication.kubernetes.io/node-name` extra key).
   A reporter on `node-A` cannot impersonate `node-B`.

2. **Constrained action role** — restricts what the impersonated identity is allowed to do.

   ```yaml
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: node-status-patcher-constrained
   rules:
   - apiGroups: [""]
     resources: ["nodes"]
     verbs: ["impersonate-on:associated-node:get"]
   - apiGroups: [""]
     resources: ["nodes/status"]
     verbs: ["impersonate-on:associated-node:update"]
   ```

   The `impersonate-on:associated-node:<verb>` verbs permit only specific operations during the impersonated session.
   The reporter can get its own Node and update its status, but cannot modify labels, taints, the spec, or any resource other than `nodes/status`.

Both roles are bound to the reporter's ServiceAccount via ClusterRoleBindings.

### Reporter Configuration

The reporter must be configured to use impersonation by setting the `IMPERSONATE_NODE` environment variable to `"true"`. When enabled, the reporter sends `Impersonate-User: system:node:<nodeName>` headers on every API request, which triggers the constrained impersonation authorization flow in the API server.

```yaml
env:
  - name: IMPERSONATE_NODE
    value: "true"
```

When `IMPERSONATE_NODE` is not set, the reporter uses its ServiceAccount identity directly (the pre-v1.35 behavior).
