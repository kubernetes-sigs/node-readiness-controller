# UI Design Document: Node Readiness Controller

### LFX Project: Headlamp Plugin for Node Readiness Controller (Term 3)

This project brings native UI visibility to the Node Readiness Controller (NRC) via a [Headlamp](https://headlamp.dev/) plugin. This is a continuation of the Term 2 observability project, which delivered the metrics foundation, scrape-time collectors, and Grafana dashboard.

The project answers two main questions:

1. **Per-node readiness state:** Why is this specific node not accepting workloads? Which rule, condition, or taint is holding it back? Today this requires `kubectl describe node`, controller logs, or raw YAML parsing.
2. **Fleet-wide rule enforcement:** Across all rules, how many nodes are matched, held by taints, or free of taints? Which rules report evaluation failures? Today this is answered only through Prometheus queries and the Grafana dashboard built in Term 2.

## Goals

* **Rule Observability (v1.0):** Provide cluster-level views of all `NodeReadinessRule` resources, reporting targeted nodes, taint enforcement states, and evaluation errors.
* **Node-Level Breakdown (Phase 2):** Provide per-node rule evaluations and condition breakdowns via the upcoming `NodeReadinessEvaluation` (NRE) CRD.
* **Node Details Extension (Phase 2):** Integrate NRC lifecycle conditions and managed taints directly into Headlamp's native Node details view.
* **Native & Performant:** Rely strictly on Headlamp's built-in Kubernetes client and watch hooks without custom daemons.
* **Ecosystem Delivery:** Validate scale boundaries and publish the plugin to the Headlamp Artifact Hub.

## Non-Goals

* Build a standalone backend API or web server. The plugin runs entirely inside Headlamp.
* Replace the Prometheus metrics and Grafana dashboard from Term 2. Headlamp gives visibility into per-object questions; Grafana answers fleet-wide SLO questions over time.
* Implement custom WebSocket or polling mechanisms. We rely on Headlamp's built-in watch hooks.

---

## Design

### 1. Personas

We design the UI around the same distinct personas used in the controller observability design.

1. **Infrastructure Owners (Cluster Operators):** They run the cluster and manage the node lifecycle. They need a Landing Page to confirm the controller's resources are available and get high-level rule visibility before drilling into individual rules.
2. **Component / Rule Owners:** They own the infrastructure components that gate node readiness (CNI, GPU drivers, CSI). They need the Rule Details View to see how their rule propagates. *(Note: For per-node evaluation details, the plugin will use NRE as the authoritative source once the NRE API is available.)*
3. **Workload Owners (Application Developers):** They run pods. They need to understand exactly which conditions or rules are preventing their pods from scheduling on a specific node. In environments where developers lack RBAC permissions to view Kubernetes `Node` objects directly, the standalone `NodeReadinessEvaluation` (NRE) Details View serves as their primary debugging surface. In clusters where developers have Node read access, the Node Detail Extension provides inline visibility directly on the Node page.

---

### 2. Code Organization

The project spans two repositories. All plugin UI code (React components, styles, tests) lives in the [Headlamp plugins repository](https://github.com/headlamp-k8s/plugins/tree/main/node-readiness-controller). Design documentation and any backend API changes live in the [NRC repository](https://github.com/kubernetes-sigs/node-readiness-controller).

| Repository | Contents |
|---|---|
| `headlamp-k8s/plugins/node-readiness-controller` | Plugin source (`src/`), `package.json`, unit tests, `CODEOWNERS`, Artifact Hub metadata. Reviewed by Headlamp and NRC maintainers. |
| `kubernetes-sigs/node-readiness-controller` | This design document (`docs/`), backend changes (e.g., `status.nodeEvaluations` on `NodeReadinessRule`). Reviewed by NRC maintainers. |

Plugin development work is tracked against [Issue #327](https://github.com/kubernetes-sigs/node-readiness-controller/issues/327) for traceability.

---

### 3. CRD Data Surface

The plugin reads from the `readiness.node.x-k8s.io/v1alpha1` API group. Both custom resources are cluster-scoped.

#### 3.1 NodeReadinessRule Fields Consumed

The plugin consumes `NodeReadinessRule` resources to extract rule specifications (`enforcementMode`, `conditionPolicy`, `taint`, `conditions`, `nodeSelector`, `dryRun`) and controller status (`dryRunResults`, `nodeEvaluations`, `failedNodes`, `appliedNodes`, `observedGeneration`). Detailed UI column-level and section-level mappings are specified in Section 5.

* **Scale & Payload Boundary:** The NRR CRD schema permits up to 5,000 items in each of `status.nodeEvaluations`, `status.failedNodes`, and `status.appliedNodes`. Standard Kubernetes list requests (`GET /apis/.../nodereadinessrules`) return full JSON objects including these status arrays. While Headlamp table pagination defers browser DOM rendering, it does *not* reduce network payload size or React client memory consumption. For clusters with extensive rule sets and wide node selectors, server-side summary aggregation (`status.evaluationSummary`) is recognized as a necessary optimization; ultimately, as NRE graduates and takes over granular per-node reporting, completely deprecating and removing `status.nodeEvaluations` from the NRR API will permanently eliminate rule-level payload bloat.
* **Stale Status Detection (`observedGeneration`):** When an operator modifies rule specifications (`spec`), Kubernetes immediately increments `metadata.generation`. Until the controller reconciles the update, `status.observedGeneration` reflects earlier specifications. When `status.observedGeneration < metadata.generation`, the UI treats displayed evaluation data as provisional/stale, rendering a `"Reconciling / Spec Modified"` indicator so operators are not misled by out-of-date evaluation records reflecting earlier rule generations. If `observedGeneration` is absent, the UI falls back to standard evaluation display without asserting reconciliation staleness.

#### 3.2 NodeReadinessEvaluation - Dependency Management & Data Contract

The `NodeReadinessEvaluation` (NRE) CRD is a per-node evaluation object currently under development ([PR #345](https://github.com/kubernetes-sigs/node-readiness-controller/pull/345)). While the first alpha release focuses on `NodeReadinessRule` (NRR) core observability, achieving the original LFX project outcome (node-level investigation: applicable rules, per-condition results, failure reasons, managed taints, and lifecycle conditions) requires clear NRE dependency management.

**Dependency Tracking & Decision Gates:**
* **Dependency Owner:** Upstream NRC maintainers / PR #345 authors.
* **Decision Checkpoint Date:** **October 28, 2026** (Midterm Evaluation boundary).
* **Agreed Contingency / Fallback:** If upstream NRE PR #345 has not merged and stabilized by the decision date, the plugin will provide node-level investigation by querying core `Node` objects directly and evaluating rule label selectors and condition requirements on the client side, or defer the dedicated NRE resource tab while continuing inline condition reporting. Upstream delays will not stall releases.
* **Opt-In Controller Flag:** In PR #345, NRE generation is opt-in via the `--enable-node-readiness-evaluation` controller flag. The plugin treats "NRE CRD installed" and "NRE evaluation data populated" as separate states.

**Evaluation Contract & Semantic Principles:**
* **API Fields Consumed:** Sourced from NRE PR #345: node identification (`spec.nodeName`), overall readiness state (`status.state`), overall conditions (`status.conditions`), per-rule evaluation array (`status.rules[]`), condition breakdowns (`status.rules[].readinessConditions[]`), diagnostic messages (`reason`, `message`), and timestamps (`firstEvaluatedAt`, `lastEvaluatedAt`).
* **Enum Standards:** Uses the raw API enum value `NotAvailable` directly from the CRD without extra string-mapping.
* **Readiness vs. Taint Presence:** An absent taint must **never** imply logical condition satisfaction or completed bootstrap (particularly for `bootstrap-only` rules where taints are permanently absent post-bootstrap). Logical condition satisfaction is derived strictly from condition evaluations, not taint absence. Missing evaluation data is clearly distinguished from zero counts or unknown states.

#### 3.3 Kubernetes Node Object Fields Consumed

The plugin reads `spec.taints` (filtering taints with `readiness.k8s.io/` prefix) and `status.conditions` from core Kubernetes Node objects to enrich node views.

---

### 4. Integration Architecture

The plugin is a standard Headlamp extension built with React and TypeScript (`@kinvolk/headlamp-plugin`). 

**Architecture philosophy:** The plugin owns only NRC-specific presentation and derived UI state. Headlamp owns Kubernetes connectivity, cluster context, navigation conventions, authentication, RBAC behavior, and shared UI primitives. No standalone backend or custom API client is required.

**Data source strategy:** `NodeReadinessRule` is the source for rule-level aggregate information. `NodeReadinessEvaluation` supplements it as the source for per-node evaluation details once the NRE API is available. Both resources coexist; NRE does not replace NRR.

**Cluster Context:**
All plugin routes and Kubernetes resource queries operate against Headlamp's currently selected cluster. The plugin must not maintain cluster-global React state for NRR/NRE objects unless keyed by cluster. Route generation should use Headlamp's Router APIs so navigation remains cluster-aware.

**State Synchronization & Reactivity:**
The UI relies strictly on Headlamp's native resource watch mechanisms (`ADDED`, `MODIFIED`, `DELETED`) to update UI state reactively without custom polling, WebSockets, or page reloads.

**Data Fetching & Cardinality:**
The Landing Page performs a single `NodeReadinessRule` list request to calculate summary statistics, deferring per-node UI exploration to deeper views to prevent client-side DOM bottlenecks. High-cardinality columns (Name, Node Selector, Taint) use substring search rather than unbounded dropdown filters to prevent browser memory exhaustion.

#### Required RBAC Permissions

The plugin requires the following minimum ClusterRole permissions for full functionality:

| API Group | Resource | Verbs | Used By |
|---|---|---|---|
| `readiness.node.x-k8s.io` | `nodereadinessrules` | `get`, `list`, `watch` | Rule List, Rule Details, Landing Page |
| `readiness.node.x-k8s.io` | `nodereadinessevaluations` | `get`, `list`, `watch` | NRE List, NRE Details, Node Extension |
| `""` (core) | `nodes` | `get`, `list`, `watch` | NRE Details (taints display), Node Extension |

> **Authorization & Preflight Handling:** Headlamp provides authorization helpers for custom resource classes through `<AuthVisible>` and `KubeObject.getAuthorization` (demonstrated in Headlamp's [Kueue plugin](https://github.com/headlamp-k8s/plugins/blob/main/kueue/src/components/common/KueueAdminResourceAccess.tsx)). While not every plugin read is preflighted automatically, the plugin leverages these mechanisms for conditional visibility and couples them with robust runtime error handling for `403 Forbidden` responses.
>
> **Node Access Isolation:** Many production environments restrict `nodes` read access to cluster administrators. When Node access is unavailable, the plugin degrades gracefully: node names in NRR/NRE views are rendered as plain text instead of clickable links, active taints in NRE Details display as `"—"`, and the Node Details Extension (Section 5.6) is silently omitted. In the Node Details Extension, the plugin reuses the Node object supplied directly by Headlamp's context rather than triggering redundant `GET /api/v1/nodes/:name` requests. Tests must explicitly verify NRE access in clusters where `nodes` read permissions are absent.

---

### 5. UI Surface

We define six primary user interfaces. Status is conveyed through text and semantic status indicators, with color used as a secondary visual cue. Views follow Headlamp's existing UI patterns and use native Headlamp/MUI components where appropriate.

#### 5.1 Landing Page

The entry point for the plugin. The Landing Page provides immediate orientation for operators, acting as a lightweight summary before drilling down into the Rule List. It performs a single `NodeReadinessRule` list request.

* **CRD & Endpoint Detection:** No separate CRD metadata fetch is performed via `CustomResourceDefinition.useGet`. In enterprise multi-tenant clusters, non-admin users often lack RBAC permissions to read `apiextensions.k8s.io/v1 CustomResourceDefinition` (returning `403 Forbidden`), even when granted read access to custom resources. Furthermore, because the Landing Page requires `NodeReadinessRule` data to populate summary cards, the single `NodeReadinessRule` list request serves dual purpose: it distinguishes unserved API endpoint / CRD-not-installed states (`404` or unserved endpoint error on list) from permission errors (`403`) and empty-resource states (`200` with zero items) in a single network round-trip. If the list request indicates that the resource endpoint is not served, the UI renders an informative `<EmptyContent>` component with a link to the [NRC installation documentation](https://node-readiness-controller.sigs.k8s.io/getting-started/).
* **Controller Deployment Health:** To confirm controller reconciliation health beyond CRD availability, the Landing Page optionally inspects the controller `Deployment` (`K8s.ResourceClasses.Deployment.useGet` targeting the controller manager in its installation namespace) to verify `status.readyReplicas > 0`. If the user lacks RBAC permissions to read Deployments in that namespace (`403 Forbidden`), the health indicator is omitted entirely (shows nothing) rather than displaying a misleading degraded or unknown badge.
* **Summary Cards:** Use Headlamp's standard summary-card/layout pattern, providing high-level context without attempting complex cluster-wide unique node analytics. All card metrics are computed **client-side in React directly from the in-memory Kubernetes `NodeReadinessRule` list response**, rather than querying Prometheus metrics (such as `node_readiness_rules` from Term 2). This ensures the plugin functions standalone on any cluster without external monitoring or Prometheus server dependencies *(Note: Client-side calculation is an initial alpha approach limited to the first two releases to accelerate delivery; native Prometheus metric integration will be evaluated in consultation with Headlamp maintainers for subsequent releases)*:
  * **Total Rules:** In-memory count of fetched `NodeReadinessRule` objects (`rules.length`).
  * **Rules with Taints Present:** Count of non-dry-run rules (`spec.dryRun !== true`) where at least one node evaluation currently reports `taintStatus === 'Present'`. Rules with `spec.dryRun: true` are strictly excluded from this count because they do not actively enforce or apply taints. (Reflects physical taint observation; logical satisfaction is evaluated via NRE).
  * **Rules with Evaluation Errors:** Count of rules where `failedNodes` is not empty (applies to both live and dry-run rules, as controller failures to evaluate nodes represent actionable errors).
  * **Counting Semantics & Partial/Stale Data:**
    * *Independent Dimensions:* "Rules with Taints Present" and "Rules with Evaluation Errors" are orthogonal metrics. A rule that has evaluated nodes reporting taints present *and* un-evaluable nodes in `failedNodes` is counted in **both** cards. If a rule has only `failedNodes` and zero completed evaluations, it contributes exclusively to evaluation errors.
    * *Stale Status (`observedGeneration < metadata.generation`):* Rules currently undergoing controller reconciliation continue to be counted based on their most recently published status evaluations, but the summary UI displays a secondary helper note or tooltip (e.g., `"Includes N reconciling"`) so operators are aware that displayed counts may reflect the prior spec generation before recent edits.

#### 5.2 Rule List View

Table of all `NodeReadinessRule` objects, utilizing Headlamp's native `ResourceListView`.

**Error States:**
* If the resource endpoint is unserved / CRD not installed (list request returns `404`), renders a Headlamp `<EmptyContent>` component with the message `"Node Readiness Controller CRDs not installed"` and a `<Link>` to the [installation guide](https://node-readiness-controller.sigs.k8s.io/getting-started/).
* If RBAC denies access (API returns `403`), renders Headlamp's native permission error view.

| Column | Source | Filterable | Component & Style |
|---|---|---|---|
| Name | `metadata.name` | Yes | Standard Headlamp `<Link>` to the Rule Details page. If `spec.dryRun: true`, a "Dry Run" chip/badge is rendered beneath the link. |
| Enforcement | `spec.enforcementMode` & `spec.conditionPolicy` | Yes | Shows the enforcement mode (e.g., `continuous`). When `conditionPolicy: anyOf`, also displays `Policy: anyOf` (omitted for the default `allOf`). |
| Status | Computed from `nodeEvaluations`, `failedNodes`, & `observedGeneration` | Yes | Headlamp `<StatusLabel>`. Computed in canonical precedence: **Terminating** (`metadata.deletionTimestamp` present) > **Reconciling** (`status.observedGeneration < metadata.generation`; warning pill indicating spec was modified) > **Eval Error** (`failedNodes` non-empty; error red pill) > **Dry-Run Evaluated** (for dry-run rules with populated simulation output; neutral grey pill) > **Bootstrap Completed** (for `bootstrap-only` rules when 100% of targeted nodes have met conditions and startup taints have been removed; green success pill) > **Fleet Satisfaction Ratio (`{satisfied}/{targeted} Satisfied`)** (numerical progress representation: green success pill when 100% of targeted nodes are satisfied e.g., `5/5 Satisfied`; warning orange pill when partially satisfied e.g., `3/5 Satisfied` or `0/5 Satisfied`; neutral grey pill `"No Matching Nodes"` when `nodeSelector` matches 0 nodes in the cluster instead of an awkward `0/0`) > **Evaluation Pending** (when evaluation records are absent and rule is newly created). *Note: Rather than reporting a single binary "Taint Present / Absent" label on a rule—which is misleading when a rule targets multiple nodes with varying taint states—the rule status conveys fleet-wide readiness progress via the numerical `{satisfied}/{targeted} Satisfied` ratio, while concrete taint counts are tracked in the adjacent column.* |
| Taints Present Nodes | Count where `taintStatus === 'Present'` (non-dry-run only) | Yes | For non-dry-run rules: standard text displaying the integer count if > 0, or literal `"-"` if 0. For dry-run rules (`spec.dryRun: true`): displays `"N/A"` (or `"—"`), as dry-run rules do not actively enforce or apply taints. Simulated affected nodes are displayed separately in the Dry-Run Results section. |
| Evaluation Errors | `failedNodes.length` | Yes | Standard text displaying the integer count if > 0. If 0, renders a literal `"-"`. |
| Node Selector | `spec.nodeSelector` | No | A flex row of summary chips. For `matchLabels`: `"{count} Labels"`. For `matchExpressions`: `"{count} Expressions"`. Wrapped in a tooltip revealing full JSON on hover. |
| Taint | `spec.taint.key` & `spec.taint.value` | Yes | Outlined chip showing the qualified taint (e.g., `readiness.k8s.io/gpu-not-ready`). Formatted as `key=value` when `spec.taint.value` is set, or just `key` when value is empty. |
| Taint Effect | `spec.taint.effect` | Yes | Dedicated column displaying the scheduling effect (`NoSchedule`, `PreferNoSchedule`, `NoExecute`). Filterable so operators can quickly isolate disruptive `NoExecute` eviction rules. |
| Age | `metadata.creationTimestamp` | No | Headlamp's standard relative-time formatting. |

*(Note on filtering: Columns should expose filter/sort-friendly primitive values where supported by Headlamp's table APIs, rather than dictating exact dropdowns or substring implementations in the design).*

#### 5.3 Rule Details View

Deep dive into a single `NodeReadinessRule`, utilizing Headlamp's `DetailsView`.

**Error States:**
* If navigating to a specific rule that does not exist or was deleted (individual GET request returns `404`), renders Headlamp's native `<EmptyContent>` with text `"Resource not found"`. (If the entire resource endpoint is unserved, displays `"API unavailable"` per Section 5.1).
* If RBAC denies access (API returns `403`), renders a custom `<EmptyContent>` explaining the lack of read permissions for `nodereadinessrules`.

**Basic Info Section:**
Rendered using Headlamp's standard resource details/header components.

| Field | Source | Component & Style |
|---|---|---|
| Name | `metadata.name` | Headlamp's standard header. |
| Creation Timestamp | `metadata.creationTimestamp` | Headlamp's standard header. |
| Labels | `metadata.labels` | Headlamp's standard header. |
| Node Selector | `spec.nodeSelector` | Flex container of summary chips. For `matchLabels`: one chip per key/value pair formatted as `key=value`. For `matchExpressions`: one chip per expression formatted as `key operator [values]`. |
| Enforcement Mode | `spec.enforcementMode` | Standard outlined chip (e.g., `"continuous"`). |
| Condition Policy | `spec.conditionPolicy` | Standard text (e.g., `"allOf"`). Defaults to `"allOf"` if field is absent. |
| Dry-Run | `spec.dryRun` | "Yes" warning chip if true. Standard text `"No"` if false. |
| Taint Managed | `spec.taint` | Standard text. Format: `{key}={value}:{effect}` when value is set, or `{key}:{effect}` when value is empty. |

**Node Status Section:**
A container displaying a flex row of metric chips conveying fleet-wide evaluation progress:
  * **Targeted:** `{count}` (default neutral chip; total nodes matched by rule selector, from `status.nodeEvaluations.length`).
  * **Satisfied:** `{count}` (success green chip; count of matched nodes that currently satisfy required rule conditions).
  * **Unsatisfied:** `{count}` (warning orange chip; count of matched nodes where rule conditions are not yet satisfied. For dry-run rules with `spec.dryRun: true`, displays `"N/A"`).
  * **Evaluation Errors:** `{count}` (error red chip; computed from `status.failedNodes.length`).

When `status.observedGeneration < metadata.generation`, a `<StatusLabel>` with `status="warning"` and text `"Reconciling"` is displayed, indicating that the controller has not yet updated status for the latest spec changes. If `status.nodeEvaluations` is empty (`[]`), the UI distinguishes an unreconciled rule (where `observedGeneration` is absent or `< metadata.generation`, displaying `"Evaluation Pending"`) from a reconciled rule whose `nodeSelector` simply matches zero nodes in the cluster (displaying `Targeted: 0`). For dry-run rules, live taint metrics display `"N/A"` and simulated node impact is displayed in the dedicated Dry-Run Results Section below.

**Configured Rule Conditions:**
Rendered using Headlamp's shared `<SimpleTable>` component, presenting the conditions specified in `spec.conditions` that the controller enforces (distinct from dynamic per-node evaluation states).

| Column | Source | Component & Style |
|---|---|---|
| Type | `spec.conditions[].type` | Standard text. |
| Required Status | `spec.conditions[].requiredStatus` | Standard neutral text or default chip (do not use semantic error badges for `False`). |
| Default Status | `spec.conditions[].defaultStatus` | Standard neutral text or default chip. If field is absent, displays text `"Unknown (implicit)"`. |

**Per-Node Investigation & Scope Boundary:**
To prevent client-side DOM rendering bottlenecks on large clusters, the Rule Details view focuses strictly on rule definitions, high-level status metrics, and controller reconciliation failures, avoiding an unbounded per-node evaluation list. Unbounded per-node exploration is deferred to the dedicated NRE List and Details views (Sections 5.4–5.5), with an `[Explore Node Evaluations in NRE]` navigation action linking operators directly to filtered evaluations.

**Evaluation Errors Table (Conditional):**
Rendered when `status.failedNodes` is non-empty, providing diagnostic details for nodes that could not be evaluated by the controller. To prevent browser freezing or page hangs on large clusters with up to 5,000 failed nodes, this table is rendered using Headlamp's `<SimpleTable>` with client-side pagination (default: 10 rows per page, selectable: `[10, 25, 50]`) and client-side node search filtering. This guarantees that DOM nodes remain strictly bounded to $\le 50$ elements at all times.

| Column | Source | Component & Style |
|---|---|---|
| Node | `status.failedNodes[].nodeName` | Headlamp `<Link>` to the node details page. |
| Reason | `status.failedNodes[].reason` | Standard text displaying the controller failure reason (e.g., `"ConditionEvaluationFailed"`). |
| Message | `status.failedNodes[].message` | Standard text displaying diagnostic failure message. |
| Last Evaluated | `status.failedNodes[].lastEvaluationTime` | Rendered using Headlamp's standard relative-time formatting. |

**Dry-Run Results Section (Conditional):**
This section is rendered **only** when `spec.dryRun: true` and `status.dryRunResults` is present. Rendered as a standard summary card with a "Dry Run" header. Absent or unpopulated fields render as `"—"` rather than `"0"` to avoid conflating a zero count with missing controller data.

| Field | Source | Component & Style |
|---|---|---|
| Affected Nodes | `status.dryRunResults.affectedNodes` | Standard text showing the count, or `"—"` if omitted. |
| Taints To Add | `status.dryRunResults.taintsToAdd` | Standard text showing the count, or `"—"` if omitted. |
| Taints To Remove | `status.dryRunResults.taintsToRemove` | Standard text showing the count, or `"—"` if omitted. |
| Risky Operations | `status.dryRunResults.riskyOperations` | Standard text showing the count, or `"—"` if omitted. Label: `"Risky Operations"`. |
| Summary | `status.dryRunResults.summary` | Standard text block displaying the controller's human-readable summary string (or `"—"` if omitted). |

**Events Section:**
Events are intentionally omitted from the initial implementation because they are not required for the core UI.

#### 5.4 Node Readiness Evaluation List View

Table of all `NodeReadinessEvaluation` objects, utilizing Headlamp's native `ResourceListView`. **Blocked on the NRE API becoming available in the target NRC release.**

> *(Provisional API Notice: The NRE CRD is currently under development in upstream NRC PR #345. Field names, status semantics, and enum values described below represent a proposed UI contract and remain provisional until confirmed against the finalized, merged CRD schema).*

**Error States & Empty States:**
* If the NRE resource endpoint is unserved / CRD not installed (list request returns `404`), show a clear empty/error state explaining that the feature requires an NRC release providing the NRE API.
* If the NRE endpoint returns `200 OK` with 0 items, display Headlamp's `<EmptyContent>` with text `"No evaluations generated yet"`, distinguishing an empty cluster evaluation state from a missing CRD or disabled feature.
* If RBAC denies access, renders Headlamp's native permission error view.

| Column | Source | Filterable | Exact Component & Style |
|---|---|---|---|
| Name | `metadata.name` | Yes (substring) | Headlamp `<Link>` to the NRE Details page. *(Note: The current proposal uses the target node name as the NRE name; this is subject to the final API design)*. Beneath it: a smaller `"View Node"` `<Link>` pointing to Headlamp's native Node details page, rendered conditionally via Headlamp's `<AuthVisible>` permission check for core `nodes`; if the user lacks node read permissions, this secondary link is omitted to prevent 403 navigation errors. |
| State | `status.state` | Yes (dropdown) | Headlamp `<StatusLabel>` using `status="success"` for `"Available"` or `status="error"` for `"NotAvailable"` (rendered directly from the CRD enum without extra string-mapping). |
| Rules Status | Computed (Satisfied vs Total, where Total is `status.rules.length`) | Yes (dropdown: all satisfied / not all) | Headlamp `<StatusLabel>`. Uses `status="success"` for `"OK (N/N)"`, `status="warning"` for `"Not Ready (X/N)"`, or `"Evaluation Pending"` when `status` is `{}` (empty). *Note: "Evaluation Pending" is a provisional client-side UI fallback heuristic displayed when the controller has not yet written evaluation records, not an authoritative controller lifecycle condition.* |
| Age | `metadata.creationTimestamp` | No | Simple `<Typography>` text rendered via Headlamp's standard relative-time/age formatting. |

**Filtering by Rule & Scalability Caveat:** When navigated from a specific Rule Details view (`[Explore Node Evaluations in NRE]`), the NRE list is pre-filtered by rule name. Kubernetes Custom Resource Definitions do not automatically support server-side field selectors for arbitrary nested array elements (such as `status.rules[].ruleName`). The plugin implementation must verify whether the finalized NRE API provides an indexed selector (e.g., via labels or dedicated top-level query parameters). If server-side selector support is unavailable upstream, client-side filtering over the fetched NRE collection is the necessary fallback; however, this is explicitly a client-side fallback rather than a scalability guarantee at large cluster scales (thousands of NRE objects).

#### 5.5 Node Readiness Evaluation Details View

Deep dive into a single node's evaluation. **Blocked on the NRE API becoming available in the target NRC release.**

*(Provisional Contract: State and rule evaluation status mappings described below are provisional UI specifications subject to verification against the merged upstream NRE schema).*

**Error States:**
* If navigating to an individual NRE that does not exist or was deleted (individual GET request returns `404`), renders Headlamp's native `<EmptyContent>` with text `"Resource not found"`.
* If RBAC denies access, renders a custom `<EmptyContent>` explaining the lack of read permissions for `nodereadinessevaluations`.

**Basic Info:**
* **Target Node:** Rendered as a `"Visit Node"` CTA `<Link>` pointing to Headlamp's native Node details page when permitted via `<AuthVisible>`; if the user lacks read permissions for core `nodes`, renders the node name as plain text without the link.
* **State:** Headlamp `<StatusLabel>` (`status="success"` for `"Available"`, `status="error"` for `"NotAvailable"`; rendered directly from the CRD enum).
* **Active Taints:** Sourced from the target Kubernetes Node object (`Node.spec.taints`). If none, visually indicate absence. If present, a flex row of standard chips. Format: `{key}={value}:{effect}` when value is set, or `{key}:{effect}` when value is empty. If the user lacks read permissions for core `Node` resources, displays `"—"` rather than failing the NRE Details view.
* **Rule Status:** A flex row of metric chips for Total, Satisfied, and Unsatisfied counts.

**Rule Evaluations & Per-Condition Evaluation Table:**
Each applicable rule displays its evaluation outcome, expandable into a concrete per-condition table reflecting `status.rules[].readinessConditions[]` and `status.conditions` from NRE PR #345:

* **Rule Row Summary (Top-Level Row):**
  Rendered using Headlamp's `<SimpleTable>` expandable row pattern:

| Column | Source | Component & Style |
|---|---|---|
| Rule Name | `status.rules[].ruleName` | Rule name (`Typography`), with a `"Visit rule"` CTA `<Link>` on the secondary line pointing directly to that Rule's details page. |
| Enforcement Mode | `status.rules[].enforcementMode` | Standard text (`continuous` / `bootstrap-only`). |
| Status | `status.rules[].status` | Headlamp `<StatusLabel>` directly from the CRD (`Satisfied`, `Unsatisfied`, `Failed`). |
| Taint | Rule taint presence on node | Outlined chip if active on this node; literal `"—"` if taint is absent. |
| Conditions | Computed satisfaction progress | Compact progress ratio: `{satisfied}/{total}` with semantic indicator (`✅` / `<StatusLabel status="success">` if satisfied, warning if unsatisfied). Appends `[anyOf]` only if `conditionPolicy === 'anyOf'`; omitted for default `allOf`. |
* **Rule Evaluation Details (Expanded Drawer):**
  When an operator clicks to expand a rule row, an inspection panel reveals detailed diagnostic messages and lifecycle timing above the per-condition breakdown:
  * **Diagnostics:**
    * **Reason:** `status.rules[].reason` (e.g., `"ConditionsNotMet"` or `"AllConditionsSatisfied"`).
    * **Message:** `status.rules[].message` (controller diagnostic string explaining the evaluation outcome).
  * **Lifecycle Timing & Durations:**
    * **Duration Metric:** Displays `"⏱️ Unblock Duration: {duration}"` when the rule is satisfied (time elapsed from taint addition to condition satisfaction), or `"⏳ Currently Blocked: {duration}"` when actively held by the taint.
    * **Evaluation Timestamps:** `First Evaluated At` (`firstEvaluatedAt`) and `Last Evaluated At` (`lastEvaluatedAt`) rendered via Headlamp's relative date formatting.
    * **Taint Lifecycle Timestamps:** `Taint Observed At`, `Taint Added At`, and `Taint Removed At` (rendered via Headlamp date/time formatting).

* **Per-Condition Evaluation Breakdown (Nested Table):**
  A nested Headlamp `<SimpleTable>` detailing the granular condition assessment for that rule:

| Column | Source | Component & Style |
|---|---|---|
| Type | `readinessConditions[].type` | Standard text (e.g., `CNIReady`, `GPUDriverReady`). |
| Satisfied | Derived (`currentStatus === requiredStatus`) | Headlamp `<StatusLabel>`: semantic success badge `"Satisfied"` if condition criteria are met; error badge `"Unsatisfied"` if not. |
| Current Status | `readinessConditions[].currentStatus` | Semantic `<StatusLabel>` or text (`True`, `False`, `Unknown`, or `NotAvailable`). |
| Required Status | `readinessConditions[].requiredStatus` | Standard neutral text or default chip (`True`, `False`, `Unknown`). |
| Default Status | `readinessConditions[].defaultStatus` | Standard neutral text or default chip. If field is absent, displays text `"Unknown (implicit)"`. |

* **Evaluation Semantics & Edge Cases:**
  * **Absent Taint $\neq$ Satisfied:** An absent taint on the node must **never** be used as a proxy for logical satisfaction. For `bootstrap-only` rules, taints remain absent post-bootstrap regardless of ongoing node state; satisfaction is evaluated strictly against `readinessConditions`.
  * **Node Scheduling State vs. Missing Data:** `status.state` reflects the node's workload scheduling availability (`Available` vs. `NotAvailable`), asserting whether the node is ready to accept pods. Genuinely missing or unpopulated evaluation fields are rendered as literal `"—"` (or `"Evaluation Pending"`), strictly distinguishing missing controller data from legitimate zero counts (`0`) or explicit readiness states.

#### 5.6 Node Details Extension

Injected into Headlamp's native Node details page via `registerDetailsViewSection`.

**Identity & Single-Fetch Strategy:** The Node Extension derives the target Node identity directly from the resource passed by Headlamp rather than performing an additional `GET /api/v1/nodes/:name` lookup. To keep the implementation clean and avoid redundant network round-trips, the extension relies on standard error handling during the direct resource fetch (`useGet`) rather than issuing separate preflight authorization requests (`SelfSubjectAccessReview`).

* **Silent Omission (`null` return):** The extension gracefully catches fetch errors and returns `null` (rendering nothing on the Node Details page) in the following cases:
  1. The NRE CRD is not installed on the cluster (`404` on CRD endpoint).
  2. The current user lacks RBAC permissions to read `nodereadinessevaluations` (`403 Forbidden` caught on fetch).
  3. No `NodeReadinessEvaluation` resource exists for the target node (individual GET returns `404`, indicating the node is unmanaged by NRC).
* **Pending Evaluation:** When an NRE object exists for the node but its `status` is empty (`{}`) or evaluations have not yet completed, the extension renders a neutral `"Evaluation in progress"` status chip rather than returning `null` or displaying misleading zero counts.

**Node Readiness Section:**
Injected into the native Node Details view as a dedicated card. Because Headlamp already provides a native "Node Conditions" table rendering standard Kubelet conditions (`Ready`, `MemoryPressure`, `DiskPressure`, `PIDPressure`), this extension avoids duplicating standard conditions and focuses strictly on NRC readiness enforcement:

* **Summary Bar:**
  * **State:** Headlamp `<StatusLabel>` (`Available`, `NotAvailable`, or `"Evaluation in progress"`).
  * **Active Taints:** Outlined chips displaying taints actively placed on this node by NRC rules (or indicating `"None"` if no NRC taints are active).
  * **Link:** A `"View Full Evaluation"` `<Link>` pointing to the dedicated NRE Details page for granular condition inspection.
* **Rules Table:** Headlamp `<SimpleTable>` with the following columns:
  * **Rule Name:** `<Typography>` text with a `<Link>` pointing to Rule Details.
  * **Enforcement Mode:** Standard text (`continuous` / `bootstrap-only`).
  * **Status:** Headlamp `<StatusLabel>` (`Satisfied`, `Unsatisfied`, `Failed`).
  * **Taint:** Standard outlined chip if active on this node; literal `"—"` if taint is absent.
  * **Reason:** Truncated when necessary, with access to the full message.


---

### 6. Error Handling and Edge Cases

| Scenario | UI Behavior |
|---|---|
| **CRD Missing / Unserved Endpoint (`404`)** | Landing Page and Rule List render `<EmptyContent>` with message `"Node Readiness Controller CRDs not installed"` and an installation guide link. NRE views render `"NRE CRD not installed"`; Node Extension returns `null` (silently omitted). |
| **RBAC Denied on CRDs (`403`)** | List views render Headlamp's native permission error banner; resource details pages render `<EmptyContent>` detailing missing read permissions (`nodereadinessrules` or `nodereadinessevaluations`). |
| **Core `Node` Access Denied (`403`)** | Node names in NRR/NRE views render as plain text rather than clickable links; active taints in NRE Details render as `"—"`; Node Extension is silently omitted. NRR and NRE views remain fully functional. |
| **Rule Spec Modified (`status.observedGeneration < metadata.generation`)** | Rule List and Rule Details show `<StatusLabel status="warning">` `"Reconciling"`, signaling that displayed status evaluations reflect the prior spec generation before recent edits. |
| **Evaluation Pending (`observedGeneration` absent / status `{}`)** | When a rule is newly created and `status.observedGeneration` is absent or unpopulated, displays neutral `"Evaluation Pending"` (NRR/NRE) or `"Evaluation in progress"` (Node Extension) with `"—"` chips. Once reconciled (`status.observedGeneration === metadata.generation`), an empty `nodeEvaluations` list legitimately represents `Targeted: 0` (no nodes matched by `nodeSelector`), strictly distinguishing pending evaluation from zero matched nodes. |
| **Controller Evaluation Errors (`status.failedNodes`)** | Rules with non-empty `failedNodes` render `<StatusLabel status="error">` `"Eval Error"` on Rule List and display diagnostic node failure breakdowns in the Rule Details Evaluation Errors Table (paginated with search filtering to safely handle up to 5,000 failed nodes without browser freezing). |
| **Dry-Run Mode (`spec.dryRun: true`)** | Rule List displays a `"Dry Run"` chip with neutral `"Dry-Run Evaluated"`; live taint metrics display `"N/A"` (strictly excluded from live unsatisfied counts). Rule Details isolates simulated impact metrics in the Dry-Run Results card. |
| **Node Missing NRE Resource** | Node Details Extension silently returns `null` to avoid cluttering unmanaged node views, distinguishing missing objects from pending evaluation records (`status: {}`). |

---

### 7. Validation & Ecosystem

* **Testing:**
  * **Unit Tests (Vitest + React Testing Library):** Validate status calculations, failure precedence, dry-run isolation, and empty/error states.
  * **Integration Tests (local `kind`):** Verify live watch updates, cluster switching, and RBAC permission handling (`403 Forbidden`).
  * **Accessibility & Theming:** Verify keyboard navigation, Headlamp light/dark theme contrast tokens, and translatable strings (`pluginI18n`).
* **Scale Testing (KWOK):**
  * Validate UI responsiveness and table pagination under simulated scale (500+ nodes, 20+ rules using KWOK) to ensure DOM rendering remains smooth.
  * Verify watch-update resilience during concurrent condition changes to ensure batched React state updates do not drop UI frames.
* **Developer Quickstart & Samples:**
  * Provide sample manifests in `test-files/` covering healthy rules, dry-run mode, and evaluation errors.
  * Include clear setup instructions in `README.md` for local testing on `kind` or `minikube`.

---

### 8. Rollout Plan & Alpha Checkpoints

To gather early community feedback and de-risk delivery, the project adopts an **iterative alpha release strategy** with three calendar-dated checkpoints instead of a rigid waterfall plan.

| Release | Timing | Deliverables & Focus | Dependencies & Gates |
|---|---|---|---|
| **First Alpha** (`v0.1.0-alpha`) | **October 18, 2026** *(Before Oct 19 Demo Day)* | **Core NRR Observability & Packaging:** Installable Rule List and Details views, correct evaluation semantics (dry-run isolation, failure precedence), 100% unit test pass, developer `README.md` Quickstart, sample manifests (`test-files/`), release archive with checksum. *(Explicitly NOT blocked on separate landing dashboard)*. | PR #1307 merged. |
| **Second Alpha** (`v0.2.0-alpha`) | **November 9, 2026** *(Midway through remaining term)* | **Node Investigation & Dashboard:** Incorporate Demo Day feedback, Landing Page summary dashboard, node-level investigation & condition breakdown (NRE views or client-side fallback per Oct 28 decision date), and RBAC preflight checks with `<AuthVisible>`. | October 28 NRE Decision Checkpoint. |
| **Final Alpha** (`v0.3.0-alpha`) | **November 27, 2026** *(Near project end)* | **Full Feature Delivery & Handoff:** Node Details Extension (lifecycle-conditions panel), KWOK load test benchmark results, accessibility (a11y) audit, Artifact Hub publishing, documented limitations, and maintenance handover. | Second Alpha feedback resolved. |

**Key Review Milestones:**
* **Monday, October 19 (Headlamp Demo Day):** Demonstrate packaged plugin installation, live rule updates without page reloads, and transparently identify NRE-dependent capabilities.
* **Tuesday, October 20 (Mentorship Sync):** Review release/demo links, completed deliverables, October 28 NRE dependency decisions, and dated milestones for the second half of Term 3.

> **Release Packaging & Distribution:** Each alpha release will provide a downloadable `.tgz` archive with SHA256 checksum, compatibility matrix, release notes, and a reproducible installation walkthrough conforming to Headlamp's [plugin distribution guidelines](https://headlamp.dev/docs/latest/development/plugins/getting-started). Maps and rule-authoring forms remain outside this read-only scope.
