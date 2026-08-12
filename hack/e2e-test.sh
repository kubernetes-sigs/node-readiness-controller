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

set -o errexit
set -o nounset
set -o pipefail

export KIND=${KIND:-kind}
export KUBECTL=${KUBECTL:-kubectl}

KIND_CLUSTER="${KIND_CLUSTER:-nrr-test}"
USE_EXISTING_CLUSTER="${USE_EXISTING_CLUSTER:-false}"
ARTIFACTS="${ARTIFACTS:-.}"
E2E_KIND_VERSION="${E2E_KIND_VERSION:-v1.36}"

if [[ "$E2E_KIND_VERSION" =~ ^v[0-9]+\.[0-9]+$ ]]; then
    K8S_VERSION=$(curl -sf --retry 3 --retry-delay 5 --max-time 30 \
        "https://api.github.com/repos/kubernetes/kubernetes/git/matching-refs/tags/${E2E_KIND_VERSION}." \
        | grep -oP 'v[0-9]+\.[0-9]+\.[0-9]+(?=")' \
        | sort -V | tail -1)
    if [ -z "$K8S_VERSION" ]; then
        echo "ERROR: could not resolve latest patch for ${E2E_KIND_VERSION}" >&2
        exit 1
    fi
else
    K8S_VERSION="$E2E_KIND_VERSION"
fi

# Use a temporary KUBECONFIG so that the script does not mess up the current user's kubeconfig.
KUBECONFIG=""

function cleanup {
    if [ "$USE_EXISTING_CLUSTER" != 'true' ]; then
        mkdir -p "$ARTIFACTS"
        $KIND export logs "$ARTIFACTS" --name "$KIND_CLUSTER" || true
        $KIND delete cluster --name "$KIND_CLUSTER" || true
        [ -n "$KUBECONFIG" ] && rm -f "$KUBECONFIG"
    fi
}

function build_node_image {
    if [ "$USE_EXISTING_CLUSTER" != 'true' ]; then
        $KIND build node-image "$K8S_VERSION" --image nrr/kind-node:"${K8S_VERSION}"
    fi
}

function startup {
    if [ "$USE_EXISTING_CLUSTER" != 'true' ]; then
        KUBECONFIG="$(mktemp)"
        if [ -z "$KUBECONFIG" ]; then
            echo "Failed to generate temporary KUBECONFIG" 1>&2
            exit 1
        fi
        export KUBECONFIG
        $KIND create cluster --name "$KIND_CLUSTER" --image nrr/kind-node:"${K8S_VERSION}" --wait 1m
        $KUBECTL get nodes > "$ARTIFACTS/kind-nodes.log" 2>/dev/null || true
        $KUBECTL describe pods -n kube-system > "$ARTIFACTS/kube-system-pods.log" 2>/dev/null || true
    fi
}

mkdir -p "$ARTIFACTS"
trap cleanup EXIT
build_node_image
startup

go test -tags=e2e ./test/e2e/ -v -ginkgo.v \
    --ginkgo.junit-report="$ARTIFACTS/junit.xml"
