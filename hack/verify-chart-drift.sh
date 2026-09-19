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

# Chart.yaml is stamped at package time by build-helm, but a plain
# `helm install ./charts/...` reads it literally, so it has to match the release
# in the VERSION file. It drifted once already and shipped the previous image.
echo "Verifying chart version metadata matches the VERSION file..."
release="$(tr -d '[:space:]' < VERSION)"
chart_app="$(sed -n 's/^appVersion: *"\{0,1\}\([^"]*\)"\{0,1\}$/\1/p' charts/node-readiness-controller/Chart.yaml | tr -d '[:space:]')"
chart_ver="$(sed -n 's/^version: *\(.*\)$/\1/p' charts/node-readiness-controller/Chart.yaml | tr -d '[:space:]')"
if [ "${chart_app}" != "${release}" ]; then
  echo "error: Chart.yaml appVersion (${chart_app}) does not match VERSION (${release})" >&2
  exit 1
fi
if [ "${chart_ver}" != "${release#v}" ]; then
  echo "error: Chart.yaml version (${chart_ver}) does not match VERSION (${release#v})" >&2
  exit 1
fi
