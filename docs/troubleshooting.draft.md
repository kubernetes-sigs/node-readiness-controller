## Troubleshooting

### Common Issues

1. **Rule conflicts**: Multiple rules targeting the same taint key
   ```sh
   # Check validation webhook logs
   kubectl logs -n nrgcontroller-system deployment/nrgcontroller-controller-manager | grep webhook
   ```

2. **Missing node conditions**: Rules waiting for conditions that don't exist
   ```sh
   # Check node conditions
   kubectl describe node <node-name> | grep Conditions -A 20

   # Check rule evaluation status
   kubectl get nodereadinessrule <rule-name> -o yaml | grep nodeEvaluations -A 50
   ```

3. **RBAC issues**: Controller can't update nodes or rules
   ```sh
   # Check controller logs for permission errors
   kubectl logs -n nrgcontroller-system deployment/nrgcontroller-controller-manager

   # Verify RBAC
   kubectl describe clusterrole nrgcontroller-manager-role
   ```

### Bootstrap Completion Tracking

For bootstrap-only rules, completion is tracked via node annotations:

```sh
# Check if bootstrap completed for a node
kubectl get node <node-name> -o jsonpath='{.metadata.annotations}'

# Look for: readiness.k8s.io/bootstrap-completed-<ruleName>=true
```

### Verification

Check that the controller is running:

```sh
kubectl get pods -n nrgcontroller-system
kubectl logs -n nrgcontroller-system deployment/nrgcontroller-controller-manager
```

Verify CRDs are installed:

```sh
kubectl get crd nodereadinessrules.readiness.node.x-k8s.io
```


### Debugging

Enable verbose logging:

```sh
# Edit controller deployment to add debug flags
kubectl patch deployment -n nrgcontroller-system nrgcontroller-controller-manager \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--zap-log-level=debug"]}]}}}}'
```