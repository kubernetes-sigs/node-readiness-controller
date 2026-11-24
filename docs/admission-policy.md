# MutatingAdmissionPolicy for DaemonSet Toleration Injection

This document describes how to deploy and use the MutatingAdmissionPolicy-based mechanism to automatically inject readiness tolerations into DaemonSets.

## Overview

When new nodes register with `readiness.k8s.io/*` taints, critical infrastructure DaemonSets (such as CNI or CSI drivers) cannot schedule unless they explicitly tolerate those taints.

The MutatingAdmissionPolicy approach uses Kubernetes native admission control with CEL (Common Expression Language) to inject tolerations **without requiring a custom mutating webhook server**.

## Requirements

> [!IMPORTANT]
> Clusters running Kubernetes < 1.36 require the `MutatingAdmissionPolicy` feature gate and `admissionregistration.k8s.io/v1beta1` runtime config enabled on the API server. In Kubernetes >= 1.36, the policy API is GA (`v1`).

- Kubernetes cluster >= 1.30
- NodeReadinessRule CRDs installed

## How It Works

1. The Node Readiness Controller continuously monitors active `NodeReadinessRule` resources.
2. Active `NoSchedule` taint keys starting with `readiness.k8s.io/` are extracted, deduplicated, and synchronized to the `readiness-taints` ConfigMap in the controller namespace.
3. When a DaemonSet is created or updated, the API server evaluates the `MutatingAdmissionPolicy`.
4. The policy reads the ConfigMap parameter resource and appends any missing readiness tolerations to `.spec.template.spec.tolerations`.

## Failure Behavior & Safety

Toleration injection is **best-effort** and will never fail-close or block cluster operations:

- `failurePolicy: Ignore` on the policy: CEL evaluation errors will not reject DaemonSet creation.
- `parameterNotFoundAction: Allow` on the binding: if the `readiness-taints` ConfigMap does not exist, the mutation is skipped and the DaemonSet is admitted unchanged.
- Safe CEL filtering: empty keys are explicitly stripped to prevent accidental cluster-wide wildcard tolerations.

## Opting Out

To prevent toleration injection on a specific DaemonSet, set the annotation:

```yaml
metadata:
  annotations:
    readiness.k8s.io/auto-tolerate: "false"
```

## Deployment

### Option 1: Helm Chart

Enable the policy when installing or upgrading the Helm chart:

```bash
helm upgrade --install node-readiness-controller ./charts/node-readiness-controller \
  --set mutatingAdmissionPolicy.enabled=true
```

### Option 2: Kustomize

```bash
# Deploy the admission policy manifests
kubectl apply -k config/admission-policy
```

> [!NOTE]
> By default, `config/admission-policy/binding.yaml` references parameter ConfigMap in `nrr-system` to match `config/default`. If deploying the controller into a custom namespace via Kustomize, ensure `spec.paramRef.namespace` in `config/admission-policy/binding.yaml` is updated accordingly.

