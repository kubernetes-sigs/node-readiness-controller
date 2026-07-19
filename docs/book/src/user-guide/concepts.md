# Concepts

This section explores the core concepts of the Node Readiness Controller and how to use it to manage node lifecycle.

## Node Readiness Rule (NRR)

The `NodeReadinessRule` is the primary resource used to define readiness criteria for your nodes. It allows you to define declarative "gates" that a node must pass before it is considered ready for workloads.

A rule specifies:
1.  **Target Nodes**: Which nodes the rule applies to (using `nodeSelector`).
2.  **Readiness Conditions**: A list of conditions (type and status) that must be met.
3.  **Readiness Taint**: The taint to apply to the node if the conditions are *not* met.

When a rule is created, the controller continuously watches all matching nodes. If a node does not satisfy the required conditions, the controller ensures the configured taint is present, preventing the scheduler from assigning new pods to that node.

### Readiness Domain and Taint Keys

Node Readiness Controller uses the `readiness.k8s.io` domain for taints and annotations that it owns. All `spec.taint.key` values in `NodeReadinessRule` must start with the `readiness.k8s.io/` prefix; this is enforced by the CRD schema.

Typical taint keys look like:
- `readiness.k8s.io/network-not-ready`
- `readiness.k8s.io/storage-driver-not-ready`
- `readiness.k8s.io/<component-name>`

The segment after `readiness.k8s.io/` should describe the dependency or subsystem whose readiness is being guarded (for example, a CNI plugin, storage backend, or security agent). Treat this domain as reserved for the controller and closely related components, and avoid reusing it for unrelated taints.

### Default Condition Status (`defaultStatus`)

When defining readiness conditions, you can configure an optional `defaultStatus` field (one of `True`, `False`, or `Unknown`) under `spec.conditions[]`. This field determines how a condition is evaluated when it is completely absent from the Node's status.

If `defaultStatus` is omitted, the controller assumes a fallback status of `Unknown`.

#### Use Case: Problem Gating (Default-Allow)
By default, the controller operates in a "default-deny" fashion. If a guarded condition is missing from a new node, it resolves to `Unknown`, which typically does not satisfy `requiredStatus: True` or `requiredStatus: False`, causing the node to be tainted.

However, for problem-detection conditions (like those reported by Node Problem Detector), you want a "default-allow" behavior: the node should start as healthy, and only be tainted if the problem condition explicitly triggers (e.g., `MaintenanceRequired=True`). 

By setting `requiredStatus: False` and `defaultStatus: False`, an absent `MaintenanceRequired` condition evaluates to `False`. It matches the required status, allowing the node to bootstrap without being blocked by race conditions or initialization delays.

#### Important Implication: Enforcement Modes
The choice of `defaultStatus` has critical interaction with the rule's `enforcementMode`:

> [!IMPORTANT]
> **`defaultStatus` is not supported with `bootstrap-only` rules.**
>
> `defaultStatus` is useful when a condition may never appear on a node in its healthy state, effectively treating its absence as a known-good signal. However, `bootstrap-only` mode exists precisely to *wait* for conditions to be explicitly reported before completing the bootstrap gate.
>
 > Because these two features serve opposing purposes, using them together can lead to unintended behavior, such as completing the bootstrap phase before a condition is actually verified. To prevent  this, the admission webhook explicitly rejects this combination.

## Enforcement Modes

The controller supports two distinct modes of enforcement, configured via `spec.enforcementMode`, to handle different operational needs.

### 1. Continuous Enforcement (`continuous`)
In this mode, the controller actively maintains the readiness guarantee throughout the entire lifecycle of the node.

*   **Behavior**:
    *   **If conditions fail**: The taint is applied immediately.
    *   **If conditions pass**: The taint is removed.
*   **Use Case**: Critical infrastructure dependencies that must *always* be healthy.
    *   *Example*: A CNI plugin or a storage daemon must be running. If they crash, you want the node effectively taken offline (tainted) immediately to prevent application failures.

### 2. Bootstrap-Only Enforcement (`bootstrap-only`)
In this mode, the controller enforces readiness only during the initial node startup (bootstrap).

*   **Behavior**:
    *   The taint is applied when the node first joins or the rule is created.
    *   The controller waits for the conditions to be met.
    *   **Once satisfied**:
        1.  The taint is removed.
        2.  A completion marker is added to the node's annotations: `readiness.k8s.io/bootstrap-completed-<ruleName>=true`.
    *   **After completion**: The controller ignores this rule for the node, even if the conditions fail later.
*   **Use Case**: One-time initialization steps.
    *   *Example*: Pre-pulling heavy container images, initializing a local cache, or performing hardware provisioning that only needs to happen once per boot.

## Readiness Condition Reporting

The Node Readiness Controller operates on **Node Conditions**. It does not perform health checks itself; rather, it reacts to the state of conditions on the Node object.

This design decouples the *policy* (the Controller) from the *health checking* (the Reporter). You have multiple options for reporting these conditions:

### Option 1: Node Problem Detector (NPD)
The [Node Problem Detector](https://github.com/kubernetes/node-problem-detector) is a standard Kubernetes add-on commonly found in many clusters. It is designed to monitor node health and update `NodeConditions` or emit `Events`.

You can extend NPD with **Custom Plugins** (Monitor Scripts) to check the status of your specific components (e.g., checking if a daemon process is running or if a local endpoint is responding).

**Why choose NPD?**
*   **Existing Infrastructure**: Leverages a tool that may already be running and authorized to update node status.
*   **Separation of Concerns**: Decouples the monitoring logic from the workload itself (no need to modify your DaemonSet manifests to add sidecars).
*   **Centralized Config**: Health checks are defined in NPD configuration rather than scattered across workload pod specs.

### Option 2: Readiness Condition Reporter
To help you integrate custom checks where NPD might not be suitable, the project includes a lightweight **Readiness Condition Reporter**. This is designed to be run as a **sidecar container** within your DaemonSet.

*   **How it works**:
    1.  It can run as a side-car container that runs in the same Pod as your workload.
    2.  It periodically checks a local http endpoint (e.g., healthz probe).
    3.  It patches the Node status with a custom Condition (e.g., `example.com/MyCustomServiceReady`).

**When to choose the Reporter?**
*   **Simplicity**: Good for simple "is this HTTP endpoint up?" checks without configuring external scripts.
*   **Direct Coupling**: Useful when you want the readiness reporting lifecycle of the component to strictly match the pod's lifecycle.

## Dry Run Mode

To reduce the operational risks while deploying new readiness rules in production, the controller includes a `dryRun` capability to first analyze the impact before actual deployment.

When `spec.dryRun: true` is set on a rule:
*   The controller evaluates all nodes against the criteria.
*   **No taints are applied or removed.**
*   The intended actions are reported in the `status.dryRunResults` field of the `NodeReadinessRule`.

This allows you to preview exactly which nodes would be affected and identifying any potential misconfigurations (like a typo in a label selector) before they impact your cluster.
