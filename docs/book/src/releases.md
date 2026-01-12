# Releases

This page details the official releases of the Node Readiness Controller.

## v0.1.0

**Date:** 2026-01-12

This is the first official release of the Node Readiness Controller.

### Release Notes

- Initial implementation of the Node Readiness Controller.
- Support for `NodeReadinessRule` API (`readiness.node.x-k8s.io/v1alpha1`).
- Defines custom readiness rules for k8s nodes based on node conditions.
- Manages node taints to prevent scheduling until readiness rules are met.
- Includes modes for bootstrap-only and continuous readiness enforcement.
- Readiness condition reporter for reporting component health.

### Images

The following container images are published as part of this release.

 | Image Name | Repository | Tag |
 |---|---|---|
 | `node-readiness-controller` | `gcr.io/k8s-staging-node-readiness-controller/node-readiness-controller` | `v0.1.0` |
 | `node-readiness-reporter` | `gcr.io/k8s-staging-node-readiness-controller/node-readiness-reporter` | `v0.1.0` |

### Installation

To install the controller, apply the `install.yaml` manifest for this version:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.1.0/install.yaml
```

This will deploy the controller into the `nrr-system` namespace in your cluster.

### Contributors

- ajaysundark
- Karthik-K-N
- Priyankasaggu11929
- sreeram-venkitesh
- Hii-Himanshu
- Serafeim-Katsaros
- arnab-logs
- Yuan-prog
- AvineshTripathi