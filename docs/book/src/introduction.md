# Node Readiness Controller

<img style="float: right; margin: auto;" width="180px" src="https://raw.githubusercontent.com/kubernetes-sigs/node-readiness-controller/main/docs/logo/node-readiness-controller-logo.svg"/>

A Kubernetes controller that provides fine-grained, declarative readiness for nodes. It ensures nodes only accept workloads when all required components (e.g., network agents, GPU drivers, storage drivers, or custom health-checks) are fully ready on the node.

Use it to orchestrate complex bootstrap steps in your node-init workflow, enforce node health, and improve workload reliability.

## What is Node Readiness Controller?

The Node Readiness Controller extends Kubernetes' node readiness model by allowing you to define additional pre-requisites for nodes (as readiness rules) based on node conditions. It automatically manages node taints to prevent scheduling until all specified conditions are satisfied.

## Why This Project?

Kubernetes nodes have a simple "Ready" condition. Modern workloads need more critical infrastructure dependencies before they can run.

With this controller you can:

- **Define custom readiness** for your workloads
- **Automatically taint and untaint nodes** based on condition status
- **Support continuous readiness enforcement** to block scheduling for fuse break scenarios
- **Integrate with existing problem-detectors** like NPD or any custom daemons/node plugins for reporting readiness

## Key Features

- **Multi-condition Rules**: Define rules that require ALL specified conditions to be satisfied
- **Flexible Enforcement**: Support for bootstrap-only and continuous enforcement modes
- **Conflict Prevention**: Validation webhook prevents conflicting taint configurations
- **Dry Run Mode**: Preview rule impact before applying changes
- **Comprehensive Status**: Detailed observability into rule evaluation and node readiness status
- **Node Targeting**: Use label selectors to target specific node types
- **Bootstrap Completion Tracking**: Prevents re-evaluation once bootstrap conditions are met

## Demo

**Node Readiness Controller in Kind cluster**

![Node Readiness Demo](https://raw.githubusercontent.com/kubernetes-sigs/node-readiness-controller/main/docs/demo.gif)

## Example Rule

```yaml
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: network-readiness-rule
spec:
  conditions:
    - type: "example.com/CNIReady"
      requiredStatus: "True"
  taint:
    key: "readiness.k8s.io/NetworkReady"
    effect: "NoSchedule"
    value: "pending"
  enforcementMode: "bootstrap-only"
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/worker: ""
```

## Getting Involved

If you're interested in participating in future discussions or development related to Node Readiness Controller, you can reach the maintainers of the project at:

- **Slack**: [#sig-node-readiness-controller](https://kubernetes.slack.com/messages/sig-node-readiness-controller) (visit [slack.k8s.io](https://slack.k8s.io) for a workspace invitation)

Open Issues / PRs / Discussions here:
- **Issues**: [GitHub Issues](https://sigs.k8s.io/node-readiness-controller/issues)
- **Discussions**: [GitHub Discussions](https://sigs.k8s.io/node-readiness-controller/discussions)

See the Kubernetes community on the [community page](http://kubernetes.io/community/). You can also engage with SIG Node at [#sig-node](https://kubernetes.slack.com/messages/sig-node) and [mailing list](https://groups.google.com/a/kubernetes.io/g/sig-node).

## Project Status

This project is currently in **alpha**. The API may change in future releases.