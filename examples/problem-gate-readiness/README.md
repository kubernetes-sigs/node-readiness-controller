# Problem-Gate (Default-Allow) Example

This example demonstrates how to configure a `NodeReadinessRule` for problem-gating (default-allow) using `defaultStatus: "False"` and `requiredStatus: "False"`.

## Quick Start

1. Apply the problem gate rule:
   ```bash
   kubectl apply -f problem-gate-rule.yaml
   ```

2. Verify rule status and node evaluation:
   ```bash
   kubectl get nodereadinessrule problem-gate-rule -o yaml
   ```

For detailed documentation, see the [Problem-Gate Guide](../../docs/book/src/examples/problem-gate-readiness.md).
