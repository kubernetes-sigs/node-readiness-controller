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

KUBECTL_ARGS="$@"

YQ_VERSION="v4.48.1"
YQ_PATH="/tmp/yq"

# Check if yq is installed, if not download it.
if [ ! -f "$YQ_PATH" ]; then
    echo "yq not found at $YQ_PATH, downloading..."
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

# Add the Falco Helm repository
helm repo add falcosecurity https://falcosecurity.github.io/charts
helm repo update

# Generate the Falco manifest
helm template falco falcosecurity/falco --namespace falco --set tty=true > falco.yaml

# Add the security-status-patcher sidecar
"$YQ_PATH" e -i \
  'select(.kind == "DaemonSet" and .metadata.name == "falco")
   .spec.template.spec.containers +=
   [load("hack/test-workloads/security-patcher-sidecar.yaml")]' falco.yaml

# Apply the manifest twice. The first time, it will create the CRDs and ServiceAccounts.
# The second time, it will create the rest of the resources, which should now be able to find the ServiceAccount.
kubectl apply $KUBECTL_ARGS -f falco.yaml || true
kubectl apply $KUBECTL_ARGS -f falco.yaml

# Apply the RBAC rules
kubectl apply $KUBECTL_ARGS -f ./falco-rbac-node-status-patch-role.yaml
