# Releases

This page details the official releases of the Node Readiness Controller.

## v0.2.0

**Date:** 2026-02-28

This release brings several new features, including a webhook component, metrics manifests natively integrated with Kustomize, and major documentation improvements.

### Release Notes

#### Features & Enhancements
- Add webhook as kustomize component ([#122](https://github.com/kubernetes-sigs/node-readiness-controller/pull/122))
- Enable metrics manifests ([#79](https://github.com/kubernetes-sigs/node-readiness-controller/pull/79)) 
- Use `status.patch` api for node updates ([#104](https://github.com/kubernetes-sigs/node-readiness-controller/pull/104))
- Mark controller as `system-cluster-critical` to prevent eviction ([#108](https://github.com/kubernetes-sigs/node-readiness-controller/pull/108))
- Enhance Dockerfiles and bump Go module version ([#113](https://github.com/kubernetes-sigs/node-readiness-controller/pull/113))
- Add `build-installer` make target to create CRD and install manifests ([#95](https://github.com/kubernetes-sigs/node-readiness-controller/pull/95), [#93](https://github.com/kubernetes-sigs/node-readiness-controller/pull/93))
- Add a pull request template ([#110](https://github.com/kubernetes-sigs/node-readiness-controller/pull/110))

#### Bug Fixes
- Fix dev-container: disable moby in newer version of debian ([#127](https://github.com/kubernetes-sigs/node-readiness-controller/pull/127))
- Add missing boilerplate headers in `metrics.go` ([#119](https://github.com/kubernetes-sigs/node-readiness-controller/pull/119))
- Update path to logo in README ([#115](https://github.com/kubernetes-sigs/node-readiness-controller/pull/115))

#### Code Cleanup & Maintenance
- Remove unused `globalDryRun` feature ([#123](https://github.com/kubernetes-sigs/node-readiness-controller/pull/123), [#130](https://github.com/kubernetes-sigs/node-readiness-controller/pull/130))
- Bump versions for devcontainer and golangci-kal ([#132](https://github.com/kubernetes-sigs/node-readiness-controller/pull/132))

#### Documentation & Examples
- Document `NoExecute` taint risks and add admission warning ([#120](https://github.com/kubernetes-sigs/node-readiness-controller/pull/120))
- Updates on getting-started guide and installation docs ([#135](https://github.com/kubernetes-sigs/node-readiness-controller/pull/135), [#92](https://github.com/kubernetes-sigs/node-readiness-controller/pull/92))
- Add example for security agent readiness ([#101](https://github.com/kubernetes-sigs/node-readiness-controller/pull/101))
- Managing CNI-readiness with node-readiness-controller and switch reporter to daemonset ([#99](https://github.com/kubernetes-sigs/node-readiness-controller/pull/99), [#116](https://github.com/kubernetes-sigs/node-readiness-controller/pull/116))
- Update cni-patcher to use `registry.k8s.io` image ([#96](https://github.com/kubernetes-sigs/node-readiness-controller/pull/96))
- Add video demo ([#114](https://github.com/kubernetes-sigs/node-readiness-controller/pull/114)) and update heptagon logo ([#109](https://github.com/kubernetes-sigs/node-readiness-controller/pull/109))
- Remove stale `docs/spec.md` ([#126](https://github.com/kubernetes-sigs/node-readiness-controller/pull/126))

### Images

The following container images are published as part of this release.

```
// Node readiness controller
registry.k8s.io/node-readiness-controller/node-readiness-controller:v0.2.0

// Report component readiness condition from the node
registry.k8s.io/node-readiness-controller/node-readiness-reporter:v0.2.0

```

### Installation

To install the CRDs, apply the `crds.yaml` manifest for this version:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.2.0/crds.yaml
```

To install the controller, apply the `install.yaml` manifest for this version:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.2.0/install.yaml
```

Alternatively, to install with metrics enabled:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.2.0/install-with-metrics.yaml
```

To install with secure metrics enabled:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.2.0/install-with-secure-metrics.yaml
```

To install with webhook enabled:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.2.0/install-with-webhook.yaml
```

Note: secure metrics and webhook requires cert-manager crds to be installed in the cluster.

This will deploy the controller into any available node in the `nrr-system` namespace in your cluster. Check [here](https://node-readiness-controller.sigs.k8s.io/user-guide/installation.html) for more installation instructions.

### Contributors

- ajaysundark
- arnab-logs
- AvineshTripathi
- GGh41th
- Hii-Himanshu
- ketanjani21
- knechtionscoding
- OneUpWallStreet
- pehlicd
- Priyankasaggu11929
- sats-23

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
