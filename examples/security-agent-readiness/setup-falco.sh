#!/bin/bash

# Copyright The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -z "$USE_NPD" ] && [ -z "$USE_NRR" ]; then
  echo "Error: Must set either USE_NPD=true or USE_NRR=true"
  echo ""
  echo "Usage:"
  echo "  USE_NPD=true $SCRIPT_DIR/setup-falco.sh    # Deploy with Node Problem Detector (NPD)"
  echo "  USE_NRR=true $SCRIPT_DIR/setup-falco.sh    # Deploy with Node Readiness Reporter (NRR)"
  exit 1
fi

if [ "$USE_NPD" = "true" ] && [ "$USE_NRR" = "true" ]; then
  echo "Error: Cannot set both USE_NPD and USE_NRR to true"
  exit 1
fi

KUBECTL_ARGS="$@"

TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

download_yq_if_needed() {
  YQ_VERSION="v4.48.1"
  YQ_PATH="$TEMP_DIR/yq"

  if [ ! -f "$YQ_PATH" ]; then
    echo "Downloading yq..."
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)
    case $ARCH in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            echo "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac
    YQ_BINARY="yq_${OS}_${ARCH}"
    curl -sL "https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/${YQ_BINARY}" -o "$YQ_PATH"
    chmod +x "$YQ_PATH"
  fi
}

echo "=== Installing Falco ==="
if [ "$USE_NPD" = "true" ]; then
  echo "Mode: Node Problem Detector (NPD) monitoring"
else
  echo "Mode: Node Readiness Reporter (NRR) sidecar"
fi

echo "Creating falco namespace..."
kubectl create namespace falco --dry-run=client -o yaml | kubectl apply -f -

echo "Adding Falco Helm repository..."
helm repo add falcosecurity https://falcosecurity.github.io/charts
helm repo update

echo "Generating Falco manifests..."
helm template falco falcosecurity/falco \
  --namespace falco \
  --set tty=true \
  --set falco.webserver.enabled=true \
  --set falco.webserver.listen_port=8765 > "$TEMP_DIR/falco.yaml"

download_yq_if_needed

if [ "$USE_NPD" = "true" ]; then
  # For NPD mode, Falco needs hostNetwork to be reachable from NPD
  # Helm chart doesn't have a value for setting pod-level hostNetwork,
  # so using yq to patch the falco daemonset
  echo "Enabling hostNetwork for Falco (required for NPD)..."

  "$YQ_PATH" e -i \
    'select(.kind == "DaemonSet" and .metadata.name == "falco")
     .spec.template.spec.hostNetwork = true' "$TEMP_DIR/falco.yaml"
fi

if [ "$USE_NRR" = "true" ]; then
  echo "Adding Node Readiness Reporter sidecar..."

  "$YQ_PATH" e -i \
    'select(.kind == "DaemonSet" and .metadata.name == "falco")
     .spec.template.spec.containers +=
     [load("'"$SCRIPT_DIR"'/nrr-variant/security-agent-patcher-sidecar.yaml")]' "$TEMP_DIR/falco.yaml"
fi

echo "Applying Falco manifests..."
kubectl apply $KUBECTL_ARGS -f "$TEMP_DIR/falco.yaml"

if [ "$USE_NRR" = "true" ]; then
  echo "Applying RBAC for Node Readiness Reporter..."
  kubectl apply $KUBECTL_ARGS -f "$SCRIPT_DIR/nrr-variant/falco-rbac-node-status-rbac.yaml"
fi

if [ "$USE_NPD" = "true" ]; then
  echo ""
  echo "=== Deploying Node Problem Detector (NPD) ==="
  kubectl apply -f "$SCRIPT_DIR/npd-variant/npd-rbac.yaml"
  kubectl apply -f "$SCRIPT_DIR/npd-variant/npd-falco-config.yaml"
  kubectl apply -f "$SCRIPT_DIR/npd-variant/npd-daemonset.yaml"
  
  echo "Adding toleration to NPD DaemonSet..."
  kubectl patch daemonset node-problem-detector-falco -n falco --type='json' -p='[
    {
      "op": "add",
      "path": "/spec/template/spec/tolerations/-",
      "value": {
        "key": "readiness.k8s.io/security-agent-ready",
        "operator": "Exists",
        "effect": "NoSchedule"
      }
    }
  ]' 2>/dev/null || echo "Toleration already exists or will be added on next update"
  
  echo "NPD deployed successfully"
fi

echo ""
echo "=== Falco installed successfully ==="
echo ""
if [ "$USE_NPD" = "true" ]; then
  echo "Next step:"
  echo "1. Apply NodeReadinessRule: kubectl apply -f $SCRIPT_DIR/npd-variant/security-agent-readiness-rule-npd.yaml"
  echo "2. Add toleration to Falco: $SCRIPT_DIR/add-falco-toleration.sh"
else
  echo "Next steps:"
  echo "1. Apply NodeReadinessRule: kubectl apply -f $SCRIPT_DIR/nrr-variant/security-agent-readiness-rule.yaml"
  echo "2. Add toleration to Falco: $SCRIPT_DIR/add-falco-toleration.sh"
fi
