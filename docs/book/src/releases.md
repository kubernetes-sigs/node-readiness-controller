# Releases

This page details the official releases of the Node Readiness Controller.

## v0.1.1

**Date:** 2026-01-19

This patch release includes important regression bug fixes and documentation updates made since v0.1.0.

### Release Notes

#### Bug or Regression
- Fix race condition where deleting a rule could leave taints stuck on nodes ([#84](https://github.com/kubernetes-sigs/node-readiness-controller/pull/84))
- Ensure new node evaluation results are persisted to rule status ([#87](https://github.com/kubernetes-sigs/node-readiness-controller/pull/87)]

#### Documentation
- Add/update Concepts documentation (enforcement modes, dry-run, condition reporting) ([#74](https://github.com/kubernetes-sigs/node-readiness-controller/pull/74))
- Add v0.1.0 release notes to docs ([#76](https://github.com/kubernetes-sigs/node-readiness-controller/pull/76))

### Images

The following container images are published as part of this release.

```
// Node readiness controller
registry.k8s.io/node-readiness-controller/node-readiness-controller:v0.1.1

// Report component readiness condition from the node
registry.k8s.io/node-readiness-controller/node-readiness-reporter:v0.1.1

```

### Installation

To install the CRDs, apply the `crds.yaml` manifest for this version:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.1.1/crds.yaml
```

To install the controller, apply the `install.yaml` manifest for this version:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.1.1/install.yaml
```

This will deploy the controller into any available node in the `nrr-system` namespace in your cluster. Check [here](https://node-readiness-controller.sigs.k8s.io/user-guide/installation.html) for more installation instructions.

### Contributors

- ajaysundark

## v0.1.0

**Date:** 2026-01-14

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

```
// Node readiness controller
registry.k8s.io/node-readiness-controller/node-readiness-controller:v0.1.0

// Report component readiness condition from the node
registry.k8s.io/node-readiness-controller/node-readiness-reporter:v0.1.0

```

### Installation

To install the CRDs, apply the `crds.yaml` manifest for this version:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.1.0/crds.yaml
```

To install the controller, apply the `install.yaml` manifest for this version:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.1.0/install.yaml
```

This will deploy the controller into any available node in the `nrr-system` namespace in your cluster. Check [here](https://node-readiness-controller.sigs.k8s.io/user-guide/installation.html) for more installation instructions.

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
