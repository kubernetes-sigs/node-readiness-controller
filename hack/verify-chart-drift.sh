#!/usr/bin/env bash

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

# This script checks that the Helm chart CRDs match controller-gen output.

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT="$(dirname "${BASH_SOURCE[0]}")/.."
cd "${KUBE_ROOT}"

make manifests

diff -u \
  config/crd/bases/readiness.node.x-k8s.io_nodereadinessrules.yaml \
  charts/node-readiness-controller/crds/nodereadinessrules.readiness.node.x-k8s.io.yaml

# ---------- RBAC drift ----------
# The chart's manager ClusterRole carries a verbatim copy of the rules from
# config/rbac/role.yaml, wrapped in BEGIN/END sentinel comments.  Extract that
# block and diff it against the generated file.
echo "Verifying chart manager RBAC matches config/rbac/role.yaml..."
diff -u \
  <(sed 's/\r$//' config/rbac/role.yaml | sed -n '/^rules:/,$p') \
  <(sed 's/\r$//' charts/node-readiness-controller/templates/rbac.yaml \
    | sed -n '/^# BEGIN GENERATED RBAC RULES$/,/^# END GENERATED RBAC RULES$/{ /^# /d; p; }')
echo "RBAC in the Helm chart matches config/rbac."
