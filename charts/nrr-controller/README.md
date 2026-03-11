# Node Readiness Controller for Kubernetes

[Node Readiness Controller](https://github.com/kubernetes-sigs/node-readiness-controller) for Kubernetes a Kubernetes controller that provides fine-grained, declarative readiness for nodes. It ensures nodes only accept workloads when all required components eg: network agents, GPU drivers, storage drivers or custom health-checks, are fully ready on the node.

## TL;DR:

```shell
helm repo add node-readiness-controller https://kubernetes-sigs.github.io/node-readiness-controller/
helm install my-release --namespace kube-system node-readiness-controller/nrr-controller
```

## Introduction

This chart bootstraps a [node-readiness-controller](https://github.com/kubernetes-sigs/node-readiness-controller) deployment on a [Kubernetes](http://kubernetes.io) cluster using the [Helm](https://helm.sh) package manager.

## Prerequisites

- Kubernetes 1.25+

## Installing the Chart

To install the chart with the release name `my-release`:

```shell
helm install --namespace kube-system my-release node-readiness-controller/nrr-controller
```

The command deploys the _node-readiness-controller_ on the Kubernetes cluster in the default configuration. The [configuration](#configuration) section lists the parameters that can be configured during installation.

> **Tip**: List all releases using `helm list`

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
| `nameOverride`                           | String to partially override `nrr-controller.fullname` template (will prepend the release name)                                 | `""`                                                              |
| `fullnameOverride`                       | String to fully override `nrr-controller.fullname` template                                                                      | `""`                                                              |
| `namespaceOverride`                      | Override the deployment namespace; defaults to .Release.Namespace                                                               | `""`                                                              |
| `replicaCount`                           | The replica count for Deployment                                                                                                | `1`                                                               |
| `leaderElection.enabled`                 | Enable leader election to support multiple replicas                                                                             | `false`                                                           |
| `priorityClassName`                      | The name of the priority class to add to pods                                                                                    | `system-cluster-critical`                                         |
| `rbac.create`                            | If `true`, create & use RBAC resources                                                                                          | `true`                                                            |
| `resources`                              | Node Readiness Controller container CPU and memory requests/limits                                                              | _see values.yaml_                                                 |
| `serviceAccount.create`                  | If `true`, create a service account                                                                                             | `true`                                                            |
| `serviceAccount.name`                    | The name of the service account to use, if not set and create is true a name is generated using the fullname template          | `nil`                                                             |
| `serviceAccount.annotations`             | Specifies custom annotations for the serviceAccount                                                                             | `{}`                                                              |
| `podAnnotations`                         | Annotations to add to the node-readiness-controller Pods                                                                        | `{}`                                                              |
| `podLabels`                              | Labels to add to the node-readiness-controller Pods                                                                             | `{}`                                                              |
| `commonLabels`                           | Labels to apply to all resources                                                                                                | `{}`                                                              |
| `podSecurityContext`                     | Security context for pod                                                                                                        | _see values.yaml_                                                 |
| `securityContext`                        | Security context for container                                                                                                  | _see values.yaml_                                                 |
| `terminationGracePeriodSeconds`          | Time to wait before forcefully terminating the pod                                                                              | `10`                                                              |
| `healthProbeBindAddress`                 | The bind address for health probes                                                                                              | `:8081`                                                           |
| `livenessProbe`                          | Liveness probe configuration for the node-readiness-controller container                                                        | _see values.yaml_                                                 |
| `readinessProbe`                         | Readiness probe configuration for the node-readiness-controller container                                                       | _see values.yaml_                                                 |
| `metrics.secure`                         | Enable secure metrics endpoint                                                                                                  | `false`                                                           |
| `metrics.bindAddress`                    | The bind address for metrics server                                                                                             | `:8443`                                                           |
| `metrics.service.port`                   | The port exposed by the metrics service                                                                                         | `8443`                                                            |
| `metrics.service.targetPort`             | The target port for the metrics service                                                                                         | `8443`                                                            |
| `metrics.certDir`                        | Directory for metrics server certificates                                                                                       | `/tmp/k8s-metrics-server/metrics-certs`                           |
| `metrics.certSecretName`                 | Name of the secret containing metrics server certificates                                                                       | `metrics-server-cert`                                             |
| `webhook.enabled`                        | Enable the webhook server                                                                                                       | `false`                                                           |
| `webhook.port`                           | The port for the webhook server                                                                                                 | `9443`                                                            |
| `webhook.service.port`                   | The port exposed by the webhook service                                                                                         | `8443`                                                            |
| `webhook.service.targetPort`             | The target port for the webhook service                                                                                         | `9443`                                                            |
| `webhook.certDir`                        | Directory for webhook server certificates                                                                                       | `/tmp/k8s-webhook-server/serving-certs`                           |
| `webhook.certSecretName`                 | Name of the secret containing webhook server certificates                                                                       | `webhook-server-certs`                                            |
| `certManager.enabled`                    | Enable cert-manager integration for automatic TLS certificate generation                                                        | `false`                                                           |
| `certManager.issuer.create`              | Create a cert-manager issuer                                                                                                    | `true`                                                            |
| `certManager.issuer.name`                | Name of the cert-manager issuer                                                                                                 | `selfsigned-issuer`                                               |
| `certManager.metricsCertificate.create`  | Create a cert-manager certificate for metrics server                                                                            | `true`                                                            |
| `certManager.metricsCertificate.name`    | Name of the metrics certificate                                                                                                 | `metrics-certs`                                                   |
| `certManager.webhookCertificate.create`  | Create a cert-manager certificate for webhook server                                                                            | `true`                                                            |
| `certManager.webhookCertificate.name`    | Name of the webhook certificate                                                                                                 | `serving-cert`                                                    |
| `validatingWebhook.enabled`              | Enable the validating webhook                                                                                                   | `false`                                                           |
| `validatingWebhook.name`                 | Name of the ValidatingWebhookConfiguration resource                                                                             | `validating-webhook-configuration`                                |
| `validatingWebhook.webhookName`          | Name of the webhook                                                                                                             | `vnodereadinessrule.kb.io`                                        |
| `validatingWebhook.failurePolicy`        | Failure policy for the webhook                                                                                                  | `Fail`                                                            |
| `validatingWebhook.sideEffects`          | Side effects for the webhook                                                                                                    | `None`                                                            |
| `validatingWebhook.path`                 | The path for the webhook                                                                                                       | `/validate-readiness-node-x-k8s-io-v1alpha1-nodereadinessrule`   |
| `validatingWebhook.admissionReviewVersions` | Admission review versions supported by the webhook                                                                          | `["v1"]`                                                          |
| `nodeSelector`                           | Node selectors to run the controller on specific nodes                                                                          | `nil`                                                             |
| `tolerations`                            | Tolerations to run the controller on specific nodes                                                                             | `nil`                                                             |
| `affinity`                               | Node affinity to run the controller on specific nodes                                                                           | `nil`                                                             |
| `nodeReadinessRules`                     | Custom NodeReadinessRule resources to create. CRD must be preinstalled or installation will fail.                              | `[]`                                                              |
