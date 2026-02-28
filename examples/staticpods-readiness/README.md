# Node Readiness Examples (Static Pods)

This example demonstrates how NRC can be used alongside [NPD (Node Problem Detector)](https://github.com/kubernetes/node-problem-detector) to make sure that all static pods are mirrored in the API server to avoid over-committing issues. See [#115325](https://github.com/kubernetes/kubernetes/issues/115325), [#47264](https://github.com/kubernetes/website/issues/47264), and [#126870](https://github.com/kubernetes/kubernetes/pull/126870) for reference.

## Deployment Steps

1. Deploy a testing cluster if you haven't already. For this example we are using a Kubernetes 1.35 Kind cluster.
   We need to also mount our problem daemon script and its respective config. On Kind this can be done using this config:

   ```yaml
   kind: Cluster
   apiVersion: kind.x-k8s.io/v1alpha4
   nodes:
     # This is a 1 cluster node for testing purposes
   - role: control-plane
     extraMounts:
      - hostPath: /path/to/check-staticpods-synced.sh
        containerPath: /opt/check-staticpods-synced.sh
      - hostPath: /path/to/staticpods-syncer.json
        containerPath: /opt/staticpods-syncer.json
      - hostPath: /path/to/staticPodsManifestsDir
        containerPath: /etc/kubernetes/manifests
   ```

2. Install NRC and apply the node readiness rule:

   ```bash
   kubectl apply -f nrr.yaml
   ```

3. Deploy NPD, either as a DaemonSet using the official Helm chart, or in standalone mode. Keep in mind that if you are going to go the Helm way, you need to modify the NPD image to include the binaries that your script depends on (in our case, we need `curl` and `kubectl`), or download them in your script (this is not recommended since you'll have to set a high timeout in the custom plugin monitor config, at least for the initial script run).

   ### Standalone Mode

   In this example, and since I have a 1-node Kind cluster, I will go the standalone way. Note that this is the default shipping method in GKE and AKS.

   ```bash
   # Exec into your kind node
   docker exec -it container_id bash

   # Download NPD (change the arch if you are working on arm!)
   curl -LO https://github.com/kubernetes/node-problem-detector/releases/download/v1.35.2/node-problem-detector-v1.35.2-linux_amd64.tar.gz
   mkdir /opt/npd && tar -xf node-problem-detector-v1.35.2-linux_amd64.tar.gz -C /opt/npd && rm -f node-problem-detector-v1.35.2-linux_amd64.tar.gz

   # Start NPD with the custom problem daemon
   /opt/npd/bin/node-problem-detector \
     --apiserver-override="https://127.0.0.1:6443?inClusterConfig=false&auth=/etc/kubernetes/admin.conf" \
     --config.custom-plugin-monitor=/opt/staticpods-syncer.json
   ```

   ### Using Helm

   Use the `values.yaml` file to override the default Helm values, and don't forget to add the required binaries to the NPD image (`curl`, `kubectl`).

   ```bash
   helm repo add deliveryhero https://charts.deliveryhero.io/
   helm repo update
   helm install --generate-name deliveryhero/node-problem-detector -f values.yaml
   ```

   > **Note:** For a more robust and reliable version of the shell script, you can check this [standalone binary](https://github.com/GGh41th/NPD-staticpods-syncer).