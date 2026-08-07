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

# This script checks that the Helm chart does not drift from config/:
#   - the bundled CRD must match controller-gen output
#   - the rendered RBAC must grant what config/rbac grants

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT="$(dirname "${BASH_SOURCE[0]}")/.."
cd "${KUBE_ROOT}"

CHART_DIR="charts/nrr-controller"
# Using the chart name as the release name makes the rendered resource names
# deterministic, so the prefix stripped below is always "<chart name>-".
RELEASE_NAME="nrr-controller"
HELM="${HELM:-helm}"

make manifests

echo "Verifying chart CRD matches controller-gen output..."
diff -u \
  config/crd/bases/readiness.node.x-k8s.io_nodereadinessrules.yaml \
  "${CHART_DIR}/crds/nodereadinessrules.readiness.node.x-k8s.io.yaml"

echo "Verifying chart RBAC matches config/rbac..."
if ! command -v "${HELM}" >/dev/null 2>&1; then
  echo "error: helm not found on PATH, needed to render the chart." >&2
  echo "Install it with 'make ensure-helm-install', or set HELM to its location." >&2
  exit 1
fi

RENDERED="$(mktemp)"
trap 'rm -f "${RENDERED}"' EXIT

"${HELM}" template "${RELEASE_NAME}" "${CHART_DIR}" --set rbac.create=true >"${RENDERED}"

go run ./hack/tools/verify-rbac-drift \
  -chart-manifest "${RENDERED}" \
  -config-dir config/rbac \
  -prefix "${RELEASE_NAME}-"
