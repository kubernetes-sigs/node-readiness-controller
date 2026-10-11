# Metrics Lifecycle

This page explains how the metrics exposed by Node Readiness Controller (NRC) are introduced, changed, deprecated and removed, so you can rely on them in dashboards and alerts. The per-metric reference is on the [Monitoring](./monitoring.md) page.

## Metric lifecycle

Alpha metric → Beta metric → Stable metric → Deprecated metric → Deleted metric

**Alpha metrics** have no long-term stability guarantees and can change. While NRC is in alpha, a metric is deprecated for at least one release before it is removed (see [Deprecating a metric](#deprecating-a-metric)).

**Beta metrics** observe a looser contract than stable metrics. They are candidates to be frozen at the `v1beta1` API. A beta metric is not renamed, and its labels are not changed, without a formal deprecation period.

**Stable metrics** are guaranteed not to change. A stable metric's name, type and labels are frozen.

**Deprecated metrics** are slated for removal, but are still published. Their help text starts with a deprecation notice.

**Deleted metrics** are no longer published and cannot be used.

The stability level of a metric is declared when the metric is added. The planned level of each metric is listed in the [observability design](https://github.com/kubernetes-sigs/node-readiness-controller/blob/main/docs/observability-design.md).

## Adding a metric

1. **Naming**: use the `node_readiness_` prefix for controller metrics and `node_readiness_reporter_` for reporter metrics.
2. **Stability level**: declare the level in your pull request. Metrics that feed alerts or dashboards may start as beta.
3. **Labels**: do not use node names as label values. Per-node detail belongs in resource status or events.
4. **Documentation**: add an entry to the [Monitoring](./monitoring.md) page in the same format as the existing entries (description, properties table, labels table).
5. **Release notes**: metric changes are listed in the release notes under "Metrics and Dashboards". 

## Deprecating a metric

When a metric is renamed, reshaped or replaced, the old metric is deprecated, not removed. A deprecated metric stays published for a period of time, based on its stability level:

- ALPHA: at least 1 release, while NRC is in alpha.
- BETA: 1 minor release, for a beta metric deprecated after the `v1beta1` freeze.
- STABLE: not defined yet. NRC has no stable metrics.

To deprecate a metric:

1. Keep publishing the old metric with the same name, type and labels.
2. Publish the replacement in the same release.
3. Mark the old metric as deprecated on the [Monitoring](./monitoring.md) page and link to the replacement.
4. Start the old metric's help text with a deprecation notice that names the replacement. For an alpha metric, use `[DEPRECATED]`, as `node_readiness_rules_total` does. For a beta metric after the freeze, use `[DEPRECATED since vX.Y]`.
5. List the deprecation in the release notes.

Take metric A as an example, assuming it is replaced by metric B and deprecated in release vX:

- In release vX, A is deprecated and is still published. B is published.
- In release vX+1 at the earliest, A can be removed.

A real example is `node_readiness_rules_total`, which is deprecated in favor of `node_readiness_rules`.

## Removing a metric

A removed metric is deleted from the code and from the [Monitoring](./monitoring.md) page, and the removal is listed in the release notes.

## Graduating a metric

Graduating to beta:

1. **Production validation**: the metric has run in production for at least one minor release, with a stable number of distinct label combinations (label cardinality).
2. **Usage**: the metric is used in alerts or dashboards.

Graduating to stable:

1. **API stability**: the underlying API has reached `v1`, or the `v1beta1` freeze.
2. **Shape validation**: the metric shape has stayed the same for two or more minor releases.

## References

This lifecycle is modeled on the Kubernetes metrics lifecycle. For background, see:

- [Metrics For Kubernetes System Components](https://kubernetes.io/docs/concepts/cluster-administration/system-metrics/)
- [Kubernetes Deprecation Policy](https://kubernetes.io/docs/reference/deprecation-policy/)
- [Instrumenting Kubernetes with Metrics](https://github.com/kubernetes/community/blob/main/contributors/devel/sig-instrumentation/metric-instrumentation.md) (SIG Instrumentation contributor guide)
- [KEP-1206](https://github.com/kubernetes/enhancements/issues/1206), [KEP-1209](https://github.com/kubernetes/enhancements/issues/1209) and [KEP-3498](https://github.com/kubernetes/enhancements/issues/3498), the proposals referenced by the [observability design](https://github.com/kubernetes-sigs/node-readiness-controller/blob/main/docs/observability-design.md)