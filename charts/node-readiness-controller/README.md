# Node Readiness Controller for Kubernetes

[Node Readiness Controller](https://github.com/kubernetes-sigs/node-readiness-controller) for Kubernetes a Kubernetes controller that provides fine-grained, declarative readiness for nodes. It ensures nodes only accept workloads when all required components eg: network agents, GPU drivers, storage drivers or custom health-checks, are fully ready on the node.

## TL;DR:

```shell
git clone https://github.com/kubernetes-sigs/node-readiness-controller.git
cd node-readiness-controller
helm install my-release --namespace nrr-system --create-namespace ./charts/node-readiness-controller
```

> Published chart releases via `registry.k8s.io` OCI are WIP.

## Introduction

This chart bootstraps a [node-readiness-controller](https://github.com/kubernetes-sigs/node-readiness-controller) deployment on a [Kubernetes](http://kubernetes.io) cluster using the [Helm](https://helm.sh) package manager.

## Prerequisites

- Kubernetes 1.25+

## Installing the Chart

To install the chart with the release name `my-release`:

```shell
helm install --namespace nrr-system --create-namespace my-release ./charts/node-readiness-controller
```

The command deploys the _node-readiness-controller_ on the Kubernetes cluster in the default configuration. The [configuration](#configuration) section lists the parameters that can be configured during installation.

> **Tip**: List all releases using `helm list`

## CRD Upgrades

Helm installs CRDs from the chart `crds/` directory during initial install, but Helm does not upgrade or delete CRDs from that directory during `helm upgrade` or `helm uninstall`. Before upgrading to a chart version that changes the `NodeReadinessRule` schema, apply the updated CRD from the release artifacts or from `charts/node-readiness-controller/crds`.

## Avoiding a duplicate installation

The controller manages node taints. Two installations reconciling the same
`NodeReadinessRule` resources will both act on the same nodes, so only one should run
in a cluster.

This is easy to do by accident on a managed cluster: the provider may run the
controller as part of a hosted control plane, where it is not visible to you.

To make the existing installation visible, the CRD carries a label naming who manages
it. The CRD is cluster scoped and shared by every install flow:

```console
$ kubectl get crd nodereadinessrules.readiness.node.x-k8s.io \
    -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}'
node-readiness-controller
```

A fresh `helm install` refuses to proceed when that CRD already exists, and names the
manager in the error. `helm upgrade` is unaffected. Set `crds.preInstallCheck=false` to
install anyway.

Providers installing the controller should override the label value in their own
pipeline, so the manager shown above identifies them rather than the upstream default.

## Uninstalling the Chart

To uninstall/delete the `my-release` deployment:

```shell
helm delete my-release
```

The command removes all the Kubernetes components associated with the chart and deletes the release.

## Configuration

The following table lists the configurable parameters of the _node-readiness-controller_ chart and their default values.

| Parameter                                | Description                                                                                                                      | Default                                                           |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| `image.repository`                       | Docker repository to use                                                                                                         | `registry.k8s.io/node-readiness-controller/node-readiness-controller` |
| `image.tag`                              | Docker tag to use                                                                                                                | `v[chart appVersion]`                                             |
| `image.pullPolicy`                       | Docker image pull policy                                                                                                         | `IfNotPresent`                                                    |
| `imagePullSecrets`                       | Docker repository secrets                                                                                                        | `[]`                                                              |
| `nameOverride`                           | String to partially override `node-readiness-controller.fullname` template (will prepend the release name)                                 | `""`                                                              |
| `fullnameOverride`                       | String to fully override `node-readiness-controller.fullname` template                                                                      | `""`                                                              |
| `namespaceOverride`                      | Override the deployment namespace; defaults to .Release.Namespace                                                               | `""`                                                              |
| `replicaCount`                           | The replica count for Deployment                                                                                                | `1`                                                               |
| `podDisruptionBudget.enabled`            | Enable PodDisruptionBudget for the controller                                                                                   | `false`                                                           |
| `podDisruptionBudget.minAvailable`       | Minimum number of available controller pods                                                                                     | `1`                                                               |
| `podDisruptionBudget.maxUnavailable`     | Maximum number of unavailable controller pods                                                                                   | `nil`                                                             |
| `controller.kubeAPIQPS`                  | Maximum QPS to the Kubernetes API server. `-1` means no limit.                                                                  | `-1`                                                              |
| `controller.kubeAPIBurst`                | Maximum burst for throttled API server requests. `-1` means no limit.                                                           | `-1`                                                              |
| `controller.nodeConcurrentReconciles`    | Maximum number of Node objects reconciled concurrently. Raise on large clusters.                                                | `1`                                                               |
| `controller.ruleConcurrentReconciles`    | Maximum number of NodeReadinessRule objects reconciled concurrently.                                                             | `1`                                                               |
| `controller.enableNodeStateMetrics`      | Enable per-rule aggregate node state metrics (`node_readiness_nodes_by_state` gauge).                                           | `false`                                                           |
| `controller.pprofBindAddress`            | Bind address for the pprof debug endpoint. Leave empty to disable.                                                              | `""`                                                              |
| `leaderElection.enabled`                 | Enable leader election to support multiple replicas                                                                             | `true`                                                            |
| `leaderElection.namespace`               | Namespace for the leader election lease. Defaults to the release namespace when empty.                                          | `""`                                                              |
| `priorityClassName`                      | The name of the priority class to add to pods                                                                                    | `system-cluster-critical`                                         |
| `crds.preInstallCheck`                   | Fail a fresh install when the `NodeReadinessRule` CRD already exists, so a second controller cannot be installed alongside one you cannot see. No effect on `helm upgrade`. | `true`                                                            |
| `rbac.create`                            | If `true`, create & use RBAC resources                                                                                          | `true`                                                            |
| `resources`                              | Node Readiness Controller container CPU and memory requests/limits                                                              | _see values.yaml_                                                 |
| `serviceAccount.create`                  | If `true`, create a service account                                                                                             | `true`                                                            |
| `serviceAccount.name`                    | The name of the service account to use, if not set and create is true a name is generated using the fullname template          | `nil`                                                             |
| `serviceAccount.annotations`             | Specifies custom annotations for the serviceAccount                                                                             | `{}`                                                              |
| `podAnnotations`                         | Annotations to add to the node-readiness-controller Pods                                                                        | `{"kubectl.kubernetes.io/default-container":"manager"}`           |
| `podLabels`                              | Labels to add to the node-readiness-controller Pods                                                                             | `{}`                                                              |
| `commonLabels`                           | Labels to apply to all resources                                                                                                | `{}`                                                              |
| `podSecurityContext`                     | Security context for pod                                                                                                        | _see values.yaml_                                                 |
| `securityContext`                        | Security context for container                                                                                                  | _see values.yaml_                                                 |
| `terminationGracePeriodSeconds`          | Time to wait before forcefully terminating the pod                                                                              | `10`                                                              |
| `healthProbeBindAddress`                 | The bind address for health probes                                                                                              | `:8081`                                                           |
| `livenessProbe`                          | Liveness probe configuration for the node-readiness-controller container                                                        | _see values.yaml_                                                 |
| `readinessProbe`                         | Readiness probe configuration for the node-readiness-controller container                                                       | _see values.yaml_                                                 |
| `metrics.enabled`                        | Enable the metrics endpoint and create the metrics Service (disabled by default, matching the controller default)               | `false`                                                           |
| `metrics.secure`                         | Serve metrics over HTTPS with token authn/authz (requires certificates, e.g. `certManager.enabled`)                             | `false`                                                           |
| `metrics.bindAddress`                    | The bind address for metrics server                                                                                             | `:8443`                                                           |
| `metrics.service.port`                   | The port exposed by the metrics service                                                                                         | `8443`                                                            |
| `metrics.service.targetPort`             | The target port for the metrics service                                                                                         | `8443`                                                            |
| `metrics.certDir`                        | Directory for metrics server certificates                                                                                       | `/tmp/k8s-metrics-server/metrics-certs`                           |
| `metrics.certSecretName`                 | Name of the secret containing metrics server certificates                                                                       | `metrics-server-cert`                                             |
| `webhook.enabled`                        | Enable the webhook server                                                                                                       | `false`                                                           |
| `webhook.port`                           | The port for the webhook server                                                                                                 | `9443`                                                            |
| `webhook.service.port`                   | The port exposed by the webhook service                                                                                         | `443`                                                             |
| `webhook.service.targetPort`             | The target port for the webhook service                                                                                         | `9443`                                                            |
| `webhook.certDir`                        | Directory for webhook server certificates                                                                                       | `/tmp/k8s-webhook-server/serving-certs`                           |
| `webhook.certSecretName`                 | Name of the secret containing webhook server certificates                                                                       | `webhook-server-certs`                                            |
| `certManager.enabled`                    | Enable cert-manager integration for automatic TLS certificate generation                                                        | `false`                                                           |
| `certManager.certificateSubject`         | Subject to add to generated cert-manager certificates                                                                           | _see values.yaml_                                                 |
| `certManager.issuer.create`              | Create a cert-manager issuer                                                                                                    | `true`                                                            |
| `certManager.issuer.name`                | Name of the cert-manager issuer                                                                                                 | `selfsigned-issuer`                                               |
| `certManager.metricsCertificate.create`  | Create a cert-manager certificate for metrics server                                                                            | `true`                                                            |
| `certManager.metricsCertificate.name`    | Name of the metrics certificate                                                                                                 | `metrics-certs`                                                   |
| `certManager.webhookCertificate.create`  | Create a cert-manager certificate for webhook server                                                                            | `true`                                                            |
| `certManager.webhookCertificate.name`    | Name of the webhook certificate                                                                                                 | `serving-cert`                                                    |
| `validatingWebhook.enabled`              | Enable the validating webhook                                                                                                   | `false`                                                           |
| `validatingWebhook.name`                 | Name of the ValidatingWebhookConfiguration resource                                                                             | `validating-webhook-configuration`                                |
| `validatingWebhook.webhookName`          | Name of the webhook                                                                                                             | `vnodereadinessrule.readiness.node.x-k8s.io`                      |
| `validatingWebhook.failurePolicy`        | Failure policy for the webhook                                                                                                  | `Fail`                                                            |
| `validatingWebhook.sideEffects`          | Side effects for the webhook                                                                                                    | `None`                                                            |
| `validatingWebhook.path`                 | The path for the webhook                                                                                                       | `/validate-readiness-node-x-k8s-io-v1alpha1-nodereadinessrule`   |
| `validatingWebhook.admissionReviewVersions` | Admission review versions supported by the webhook                                                                          | `["v1"]`                                                          |
| `nodeSelector`                           | Node selectors to run the controller on specific nodes                                                                          | `nil`                                                             |
| `tolerations`                            | Tolerations for the controller pods (defaults tolerate all `NoSchedule`/`NoExecute` taints so the controller can run on gated nodes) | _see values.yaml_                                                 |
| `affinity`                               | Node affinity to run the controller on specific nodes                                                                           | `nil`                                                             |
| `nodeReadinessRules`                     | Custom NodeReadinessRule resources to create. When validating webhooks are enabled, apply rules after the webhook is ready.    | `[]`                                                              |
