# Releases

This page details the official releases of the Node Readiness Controller.

## v0.3.0

**Date:** 2026-03-18

This release focuses on security hardening, observability, and flexibility. Key updates include immutability for `NodeReadinessRule` spec fields, constrained impersonation for secure node status updates, and support for static pod installation flows. It also introduces node events for taint operations and several maintenance updates to address vulnerabilities.

### Release Notes

#### Features & Enhancements
- Make `NodeReadinessRule` spec fields immutable ([#164](https://github.com/kubernetes-sigs/node-readiness-controller/pull/164))
- Add graceful shutdown and propagate context in readiness-condition-reporter ([#174](https://github.com/kubernetes-sigs/node-readiness-controller/pull/174))
- Propagate context and use merge patch in bootstrap completion tracking ([#173](https://github.com/kubernetes-sigs/node-readiness-controller/pull/173))
- Improve security posture by pruning unnecessary RBAC ([#172](https://github.com/kubernetes-sigs/node-readiness-controller/pull/172))
- Add CEL validation for taint key format against Kubernetes qualified name rule ([#155](https://github.com/kubernetes-sigs/node-readiness-controller/pull/155))
- Support static pod installation flow for control-plane nodes ([#162](https://github.com/kubernetes-sigs/node-readiness-controller/pull/162))
- Add Podman support ([#157](https://github.com/kubernetes-sigs/node-readiness-controller/pull/157))
- Constrained impersonation for secure node status updates ([#143](https://github.com/kubernetes-sigs/node-readiness-controller/pull/143))
- Add node events for taint operations (TaintAdded, TaintRemoved, TaintAdopted) ([#158](https://github.com/kubernetes-sigs/node-readiness-controller/pull/158))
- Restrict `NodeReadinessRuleSpec.Taint` to "readiness.k8s.io/" prefix ([#112](https://github.com/kubernetes-sigs/node-readiness-controller/pull/112))
- Add TLS and webhook installation support to Makefile ([#146](https://github.com/kubernetes-sigs/node-readiness-controller/pull/146))

#### Code Cleanup & Maintenance
- Update `manager.yaml` to modify nodeSelector and tolerations ([#129](https://github.com/kubernetes-sigs/node-readiness-controller/pull/129))
- Bump golang version to address vulnerabilities ([#169](https://github.com/kubernetes-sigs/node-readiness-controller/pull/169))
- Fix linter and bump golangci-lint version ([#168](https://github.com/kubernetes-sigs/node-readiness-controller/pull/168))
- CVE fix: update otel sdk to 1.40.0 ([#170](https://github.com/kubernetes-sigs/node-readiness-controller/pull/170))
- Add release automation workflow ([#144](https://github.com/kubernetes-sigs/node-readiness-controller/pull/144))

#### Documentation & Examples
- Add NPD (node problem detector) variant for security-agent-readiness example ([#154](https://github.com/kubernetes-sigs/node-readiness-controller/pull/154))
- Add link checker to fix broken links in markdown ([#140](https://github.com/kubernetes-sigs/node-readiness-controller/pull/140))
- Update release notes for checking image promotion ([#149](https://github.com/kubernetes-sigs/node-readiness-controller/pull/149))
- Add controller metrics reference ([#153](https://github.com/kubernetes-sigs/node-readiness-controller/pull/153))
- Add installation steps for deploy-full target ([#147](https://github.com/kubernetes-sigs/node-readiness-controller/pull/147))
- Update `Test_README` file with small format change
  ([#145](https://github.com/kubernetes-sigs/node-readiness-controller/pull/145))
- Fix NodeReadinessGates KEP number - KEP-5233 ([#156](https://github.com/kubernetes-sigs/node-readiness-controller/pull/156))

### Images

The following container images are published as part of this release.

```
// Node readiness controller
registry.k8s.io/node-readiness-controller/node-readiness-controller:v0.3.0

// Report component readiness condition from the node
registry.k8s.io/node-readiness-controller/node-readiness-reporter:v0.3.0
```

### Contributors

- ajaysundar.k
- Ali Abbasi Alaei
- Anish Ramasekar
- Avinesh Tripathi
- Karthik Bhat
- Mohammad Faraz
- Priyanka Saggu
- Rohit Chaudhari
- Sathvik S
- Swarom

## v0.2.0

**Date:** 2026-02-28

This release brings several new features, including a validating admission webhook that validates `NodeReadinessRule` configurations, prevents conflicting rules with overlapping node selectors, and warns against risky `NoExecute` enforcement. It also introduces metrics manifests natively integrated with Kustomize, which includes support for secure metrics via TLS. Finally, this release includes major documentation improvements.

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

**Prerequisites**: If you plan to install with all optional features enabled (`install-full.yaml`), you must have [cert-manager](https://cert-manager.io/docs/installation/) installed in your cluster.

To install the CRDs, apply the `crds.yaml` manifest for this version:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.2.0/crds.yaml
```

To install the controller, choose one of the following manifests based on your requirements:

| Manifest | Contents | Prerequisites |
| :--- | :--- | :--- |
| **`install.yaml`** | Core Controller | None |
| **`install-full.yaml`** | Core Controller + Metrics (Secure) + Validation Webhook | `cert-manager` |

**Standard Installation (Minimal):**
The simplest way to deploy the controller with no external dependencies.

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.2.0/install.yaml
```

**Full Installation (Production Ready):**
Includes secure metrics (TLS-protected) and validating webhooks for rule conflict prevention. **Requires [cert-manager](https://cert-manager.io/docs/installation/)** to be installed in your cluster.

```sh
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/v0.2.0/install-full.yaml
```

This will deploy the controller into any available node in the `nrr-system` namespace in your cluster. Check [here](https://node-readiness-controller.sigs.k8s.io/user-guide/installation.html) for more detailed installation instructions.

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

This will deploy the controller into any available node in the `nrr-system`
namespace in your cluster. Check
[here](https://node-readiness-controller.sigs.k8s.io/user-guide/installation.html)
for more installation instructions.

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
