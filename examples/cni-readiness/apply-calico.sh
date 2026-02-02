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

# Download the Calico manifest
curl -sL https://raw.githubusercontent.com/projectcalico/calico/v3.30.1/manifests/calico.yaml -o calico.yaml

# Apply the manifest twice. The first time, it will create the CRDs and ServiceAccounts.
# The second time, it will create the rest of the resources, which should now be able to find the ServiceAccount.
kubectl apply $KUBECTL_ARGS -f calico.yaml || true
kubectl apply $KUBECTL_ARGS -f calico.yaml

# Apply the CNI readiness reporter DaemonSet
kubectl apply $KUBECTL_ARGS -f ./examples/cni-readiness/cni-reporter-ds.yaml

# Apply the RBAC rules
kubectl apply $KUBECTL_ARGS -f ./examples/cni-readiness/calico-rbac-node-status-patch-role.yaml
