# UI Design Document: Node Readiness Controller

### LFX Project: Headlamp Plugin for Node Readiness Controller (Term 3)

This project brings native UI visibility to the Node Readiness Controller (NRC) via a [Headlamp](https://headlamp.dev/) plugin. This is a continuation of the Term 2 observability project, which delivered the metrics foundation, scrape-time collectors, and Grafana dashboard.

The project answers two main questions:

1. **Per-object readiness state:** Why is this specific node not accepting workloads? Which rule, condition, or taint is holding it back? Today this requires `kubectl describe node`, controller logs, or raw YAML parsing.
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

The plugin consumes `NodeReadinessRule` resources to extract rule specifications (`enforcementMode`, `conditionPolicy`, `taint`, `conditions`, `nodeSelector`, `dryRun`) and controller status (`dryRunResults`, `nodeEvaluations`, `failedNodes`, `appliedNodes`, `observedGeneration`). Detailed UI column- and section-level mappings are specified in Section 5.

* **Scale & Payload Boundary:** The NRR CRD schema permits up to 5,000 items in each of `status.nodeEvaluations`, `status.failedNodes`, and `status.appliedNodes`. Standard Kubernetes list requests (`GET /apis/.../nodereadinessrules`) return full JSON objects including these status arrays. While Headlamp table pagination defers browser DOM rendering, it does *not* reduce network payload size or React client memory consumption. For clusters with extensive rule sets and wide node selectors, server-side summary aggregation (`status.evaluationSummary`) or server-side projection is recognized as an upstream optimization to eliminate client payload bloat.
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
* **Enum Standards:** Uses the API value `NotAvailable` for missing/unready data (rendered in UI as `"Not Available"`).
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

* **CRD & Endpoint Detection:** No separate CRD metadata fetch is performed. The single `NodeReadinessRule` list request serves as initial API detection: the plugin distinguishes unserved API endpoint / CRD-not-installed states (`404` or unserved endpoint error on list) from permission errors (`403`) and empty-resource states (`200` with zero items). If the list request indicates that the resource endpoint is not served, show an appropriate unavailable state (e.g., a Headlamp `<EmptyContent>` component with the message: `"Node Readiness Controller CRDs not installed"`, alongside a standard Headlamp `<Link>` pointing to the [NRC installation documentation](https://node-readiness-controller.sigs.k8s.io/getting-started/)). If the plugin client cannot definitively distinguish an unserved API group from an endpoint routing anomaly, it renders a neutral message (`"Node Readiness Controller API unavailable"`).
* **Controller Health Limitation:** CRD/API availability confirms that the resource API is available but does not guarantee that the controller is healthy or actively reconciling. Controller health detection is outside the initial plugin scope.
* **Summary Cards:** Use Headlamp's standard summary-card/layout pattern, providing high-level context without attempting complex cluster-wide unique node analytics. 
  * **Total Rules:** Count of `NodeReadinessRule` objects.
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
| Status | Computed from `nodeEvaluations`, `failedNodes`, & `observedGeneration` | Yes | Headlamp `<StatusLabel>`. Computed in canonical precedence: **Terminating** (`metadata.deletionTimestamp` present) > **Reconciling** (`status.observedGeneration < metadata.generation`; warning pill indicating spec was modified) > **Eval Error** (`failedNodes` non-empty) > **Dry-Run Evaluated** (for dry-run rules with populated simulation output; neutral grey pill) > **Taint Present** (when any evaluated node reports `taintStatus === 'Present'`; warning pill) > **Taint Absent** (when all evaluated nodes report `taintStatus === 'Absent'`; neutral grey pill, reserving semantic success styling for verified logical satisfaction in NRE) > **Status Unknown** (when records exist but contain missing or unrecognized status values) > **Evaluation Pending** (when evaluation records are absent). *Populated simulation results indicate that simulation output exists rather than asserting a successful readiness outcome. Do not infer 'Healthy' or 'Satisfied' solely from an absent taint (especially for bootstrap-only rules where taints remain absent post-bootstrap). Concrete taint states ('Taint Present' / 'Taint Absent') are reported directly; logical 'Satisfied'/'Unsatisfied' classifications are reserved for Phase 2 NRE.* |
| Taints Present Nodes | Count where `taintStatus === 'Present'` (non-dry-run only) | Yes | For non-dry-run rules: standard text displaying the integer count if > 0, or literal `"-"` if 0. For dry-run rules (`spec.dryRun: true`): displays `"N/A"` (or `"—"`), as dry-run rules do not actively enforce or apply taints. Simulated affected nodes are displayed separately in the Dry-Run Results section. |
| Evaluation Errors | `failedNodes.length` | Yes | Standard text displaying the integer count if > 0. If 0, renders a literal `"-"`. |
| Node Selector | `spec.nodeSelector` | No | A flex row of summary chips. For `matchLabels`: `"{count} Labels"`. For `matchExpressions`: `"{count} Expressions"`. Wrapped in a tooltip revealing full JSON on hover. |
| Taint | `spec.taint` | Yes | Standard outlined chip. Text format: `{key}={value}:{effect}` when `spec.taint.value` is set, or `{key}:{effect}` when value is empty. |
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
A container displaying a flex row of metric chips:
  * **Rule Evaluations:** `{count}` (default chip; total count from `status.nodeEvaluations.length`)
  * **Taint Absent:** `{count}` (neutral default grey chip; count where `taintStatus === 'Absent'`. An absent taint indicates physical taint absence and does not by itself prove node health or condition satisfaction)
  * **Taint Present:** `{count}` (warning chip; count where `taintStatus === 'Present'`. For dry-run rules with `spec.dryRun: true`, displays `"N/A"` because dry-run rules do not actively apply or remove live taints)
  * **Evaluation Errors:** `{count}` (error chip; computed from `status.failedNodes.length`)

When `status.observedGeneration < metadata.generation`, a `<StatusLabel>` with `status="warning"` and text `"Reconciling"` is displayed, indicating that the controller has not yet updated status for the latest spec changes. If `status.nodeEvaluations` is absent for a non-dry-run rule, the UI clearly distinguishes that evaluation is pending rather than displaying zero counts. For dry-run rules, live taint satisfaction metrics display `"N/A"` and simulated node impact is displayed in the dedicated Dry-Run Results Section below.

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
Rendered when `status.failedNodes` is non-empty, providing diagnostic details for nodes that could not be evaluated by the controller.

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
| Name | `metadata.name` | Yes (substring) | Headlamp `<Link>` to the NRE Details page. *(Note: The current proposal uses the target node name as the NRE name; this is subject to the final API design)*. Beneath it: a smaller `"View Node"` `<Link>` pointing to Headlamp's native Node details page. |
| State | `status.state` | Yes (dropdown) | Headlamp `<StatusLabel>` using `status="success"` for `"Available"` or `status="error"` for `"Not Available"` (provisional UI contract subject to finalized CRD values). |
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
* **Target Node:** Rendered as a `"Visit Node"` CTA `<Link>` pointing to Headlamp's native Node details page. If the user lacks read permissions for core `nodes`, renders the node name as plain text without crashing.
* **State:** Headlamp `<StatusLabel>` (`status="success"` for `"Available"`, `status="error"` for `"Not Available"`; provisional mapping).
* **Active Taints:** Sourced from the target Kubernetes Node object (`Node.spec.taints`). If none, visually indicate absence. If present, a flex row of standard chips. Format: `{key}={value}:{effect}` when value is set, or `{key}:{effect}` when value is empty. If the user lacks read permissions for core `Node` resources, displays `"—"` rather than failing the NRE Details view.
* **Rule Status:** A flex row of metric chips for Total, Satisfied, and Unsatisfied counts.

**Rule Evaluations & Per-Condition Evaluation Table:**
Each applicable rule displays its evaluation outcome, expandable into a concrete per-condition table reflecting `status.rules[].readinessConditions[]` and `status.conditions` from NRE PR #345:

* **Rule Row Summary:**
  * **Rule Name** (`Typography`), **Rule Status** (`<StatusLabel>`: `"Satisfied"`, `"Unsatisfied"`, or `"Failed"`), **Taint Managed** (outlined chip), **Policy** (text), **Timestamps** (`firstEvaluatedAt`, `lastEvaluatedAt` rendered with Headlamp's date formatting).
* **Per-Condition Evaluation Breakdown (Expanded Table):**
  A nested Headlamp `<SimpleTable>` detailing the granular condition assessment for that rule:

| Column | Source | Component & Style |
|---|---|---|
| Condition Type | `readinessConditions[].type` | Standard text (e.g., `CNIReady`, `GPUDriverReady`). |
| Required Status | `readinessConditions[].requiredStatus` | Standard text (`True`, `False`, `Unknown`). |
| Current Status | `readinessConditions[].currentStatus` | Semantic `<StatusLabel>` or text (`True`, `False`, `Unknown`, or `NotAvailable` rendered as `"Not Available"`). |
| Condition Match | Derived (`currentStatus === requiredStatus`) | `<StatusLabel status="success">` `"Matched"` or `<StatusLabel status="error">` `"Unmatched"`. |
| First Evaluated | `readinessConditions[].firstEvaluatedAt` | Relative date/time label. |
| Last Evaluated | `readinessConditions[].lastEvaluatedAt` | Relative date/time label. |

* **Evaluation Semantics & Edge Cases:**
  * **Absent Taint $\neq$ Satisfied:** An absent taint on the node must **never** be used as a proxy for logical satisfaction. For `bootstrap-only` rules, taints remain absent post-bootstrap regardless of ongoing node state; satisfaction is evaluated strictly against `readinessConditions`.
  * **Missing vs. Zero:** Missing evaluation data is rendered as `"Not Available"` (API: `NotAvailable`), clearly distinguished from zero counts or empty arrays.

#### 5.6 Node Details Extension

Injected into Headlamp's native Node details page via `registerDetailsViewSection`.

**Identity & Node Fetch Avoidance:** The Node Extension derives the target Node identity from the resource passed by Headlamp rather than performing an additional `GET /api/v1/nodes/:name` lookup, preventing redundant API calls.

**Authorization & RBAC:** Headlamp provides authorization checks for custom resource classes via `<AuthVisible>` and `KubeObject.getAuthorization` (demonstrated in [Kueue](https://github.com/headlamp-k8s/plugins/blob/main/kueue/src/components/common/KueueAdminResourceAccess.tsx)). The plugin leverages these checks to conditionally display the section without spamming the API server with unauthorized requests when a user lacks access:
* **Silent Omission (`null` return):** The extension returns `null` (rendering nothing on the Node Details page) in the following cases:
  1. The NRE CRD is not installed on the cluster (`404` on CRD endpoint).
  2. The current user lacks RBAC permissions to read `nodereadinessevaluations` (preflighted via `AuthVisible` / `403 Forbidden`).
  3. No `NodeReadinessEvaluation` resource exists for the target node (individual GET returns `404`).
* **Pending Evaluation:** When an NRE object exists for the node but its `status` is empty (`{}`) or evaluations have not yet completed, the extension renders a neutral `"Evaluation Pending"` status chip rather than returning `null` or displaying misleading zero counts.

**Node Readiness Section:**
This section introduces a unified lifecycle-conditions panel. Standard Kubernetes conditions will be shown alongside conditions referenced by NRC rules for a holistic view of the node's health. The conditions configured in NRC's rules are read from the Node object's `status.conditions`; they are grouped separately from the standard kubelet conditions for presentation only.

*Example rendering layout:*
```text
Node Conditions
Standard Kubernetes Conditions
--------------------------------
Ready              True
MemoryPressure     False
DiskPressure       False
PIDPressure        False

Conditions Referenced by NRC Rules
--------------------------------
CNIReady           False
GPUReady           True
CSIReady           Unknown
```
* **State:** Headlamp `<StatusLabel>` (`status="success"` or `status="error"`).
* **Link:** A `"View Full Evaluation"` `<Link>` pointing to the NRE Details page.
* **Rules Table:** Headlamp `<SimpleTable>` with the following columns:
  * **Rule Name:** `<Typography>` text.
  * **Status:** `<StatusLabel>`.
  * **Taint:** Standard outlined chip.
  * **Reason:** Truncated when necessary, with access to the full message.


---

### 6. Error Handling and Edge Cases

| Scenario | UI Behavior |
|---|---|
| **CRD Missing / Unserved Endpoint (`404`)** | Landing Page and Rule List render `<EmptyContent>` with message `"Node Readiness Controller CRDs not installed"` and an installation guide link. NRE views render `"NRE CRD not installed"`; Node Extension returns `null` (silently omitted). |
| **RBAC Denied on CRDs (`403`)** | List views render Headlamp's native permission error banner; resource details pages render `<EmptyContent>` detailing missing read permissions (`nodereadinessrules` or `nodereadinessevaluations`). |
| **Core `Node` Access Denied (`403`)** | Node names in NRR/NRE views render as plain text rather than clickable links; active taints in NRE Details render as `"—"`; Node Extension is silently omitted. NRR and NRE views remain fully functional. |
| **Rule Spec Modified (`status.observedGeneration < metadata.generation`)** | Rule List and Rule Details show `<StatusLabel status="warning">` `"Reconciling"`, signaling that displayed status evaluations reflect the prior spec generation before recent edits. |
| **Evaluation Pending / Empty Status (`{}`)** | When `status` is empty or unpopulated, displays neutral `"Pending"` (NRR) or `"Evaluation Pending"` (NRE/Node Extension) with `"—"` chips, strictly avoiding false zero counts or failed error states. |
| **Controller Evaluation Errors (`status.failedNodes`)** | Rules with non-empty `failedNodes` render `<StatusLabel status="error">` `"Eval Error"` on Rule List and display diagnostic node failure breakdowns in the Rule Details Evaluation Errors Table. |
| **Dry-Run Mode (`spec.dryRun: true`)** | Rule List displays a `"Dry Run"` chip with neutral `"Dry-Run Evaluated"`; live taint metrics display `"N/A"` (strictly excluded from live unsatisfied counts). Rule Details isolates simulated impact metrics in the Dry-Run Results card. |
| **Node Missing NRE Resource** | Node Details Extension silently returns `null` to avoid cluttering unmanaged node views, distinguishing missing objects from pending evaluation records (`status: {}`). |

---

### 7. Validation & Ecosystem

* **Incremental Test Matrix:** Tests accompany each feature rather than being deferred to final polish:
  * Unit tests (Vitest + React Testing Library) validate status calculations, failure precedence, dry-run isolation, and empty/error states.
  * Integration tests on a local `kind` cluster verify live watch updates, cluster switching, and RBAC permission denials (`403 Forbidden`).
  * Continuous accessibility: Keyboard navigation, semantic status indicators, light/dark theme contrast (using Headlamp theme tokens), and translatable strings (`pluginI18n`) are verified incrementally.
* **Measurable Scale & Stress Validation:** 
  Client-side table pagination defers DOM rendering but does *not* reduce network payload size or browser memory consumption. To validate scale boundaries, the plugin will be tested using **KWOK (Kubernetes WithOut Kubelet)** and Headlamp's `load-tests/` framework:
  * **Benchmark Target:** 500+ simulated nodes and 20+ rules with overlapping label selectors.
  * **Measured Metrics:** Total JSON payload size (target < 2 MB), initial render latency (target < 300 ms), browser heap memory consumption (target < 50 MB), and resilience under watch-update bursts.
* **Developer Quickstart & Sample Manifests:**
  * The repository includes a `test-files/` folder with realistic YAML manifests covering healthy rules, dry-run simulation, and evaluation failure scenarios.
  * A clear "Getting Started" guide in `README.md` provides reproducible instructions for developers to install, run, and test the plugin locally against kind or minikube.

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
