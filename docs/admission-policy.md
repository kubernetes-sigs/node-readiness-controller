# MutatingAdmissionPolicy for DaemonSet Toleration Injection

This document describes how to deploy and use the MutatingAdmissionPolicy-based approach for automatically injecting readiness tolerations into DaemonSets.

## Overview

The MutatingAdmissionPolicy approach uses Kubernetes's native admission control mechanism with CEL (Common Expression Language) to inject tolerations **without running a webhook server**. This provides a simpler, more declarative alternative to the webhook-based approach.

## Requirements

> [!IMPORTANT]
> MutatingAdmissionPolicy is needed to be enabled in the cluster.

- Feature gate: `MutatingAdmissionPolicy=true`
- Runtime config: `admissionregistration.k8s.io/v1alpha1=true`
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
