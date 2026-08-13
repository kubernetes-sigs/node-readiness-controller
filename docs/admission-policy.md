# MutatingAdmissionPolicy for DaemonSet Toleration Injection

This document describes how to deploy and use the MutatingAdmissionPolicy-based approach for automatically injecting readiness tolerations into DaemonSets.

## Overview

The MutatingAdmissionPolicy approach uses Kubernetes's native admission control mechanism with CEL (Common Expression Language) to inject tolerations **without running a webhook server**. This provides a simpler, more declarative alternative to the webhook-based approach.

## Requirements

> [!IMPORTANT]
> MutatingAdmissionPolicy is needed to be enabled in the cluster.

- Feature gate: `MutatingAdmissionPolicy=true`
- Runtime config: `admissionregistration.k8s.io/v1beta1=true`
- `kubectl` configured to access your cluster
- NodeReadinessRule CRDs installed

## Architecture

```
User applies DaemonSet
    ↓
API Server evaluates CEL policy
    ↓
Fetches Tolerations ConfigMap which contains the tolerations to be injected
    ↓
Injects tolerations (if applicable)
    ↓
DaemonSet created with tolerations
```

## Failure behavior

Toleration injection is **best-effort** and never fail-closed. A cluster stays
functional even if the controller is not installed, has not reconciled yet, or is
unhealthy:

- `failurePolicy: Ignore` on the policy — a CEL evaluation error does not reject
  the DaemonSet.
- `parameterNotFoundAction: Allow` on the binding — if the `readiness-taints`
  ConfigMap does not exist, the mutation is skipped and the DaemonSet is admitted
  unchanged.

The trade-off is that a DaemonSet created before the first NodeReadinessRule
exists is admitted without readiness tolerations. Because the policy also matches
`UPDATE`, the tolerations are injected on the next update to the DaemonSet;
otherwise add them manually.

Opt out per DaemonSet with the annotation
`readiness.k8s.io/auto-tolerate: "false"`.

## Deployment

### Option 1: Using kustomize

```bash
# Install CRDs first
make install

# Deploy the admission policy
kubectl apply -k config/admission-policy
```

### Option 2: Direct kubectl apply

```bash
# Install CRDs first
make install

# Deploy policy and binding
kubectl apply -f config/admission-policy/policy.yaml
kubectl apply -f config/admission-policy/binding.yaml
```
