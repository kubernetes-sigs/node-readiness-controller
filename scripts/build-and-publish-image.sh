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
set -euo pipefail


REPO_ROOT=$(git rev-parse --show-toplevel)
cd "$REPO_ROOT"

if [[ -z ${IMG_PREFIX:-} ]]; then
  echo "IMG_PREFIX is not set"
  exit 1
fi

if [[ -z ${IMG_TAG:-} ]]; then
  # Use a tag if the current commit is a tag, otherwise use a date+git-hash tag
  if git describe --exact-match --tags HEAD >/dev/null 2>&1; then
    IMG_TAG=$(git describe --exact-match --tags HEAD)
  else
    IMG_TAG="$(date +v%Y%m%d)-$(git rev-parse --short HEAD)"
  fi
fi
echo "Using IMG_TAG=${IMG_TAG}"

COMPONENT=${COMPONENT:-controller}

if [[ "$COMPONENT" == "controller" ]]; then
  echo "Building controller..."
  IMG_TAG=${IMG_TAG} IMG_PREFIX=${IMG_PREFIX}/node-readiness-controller make docker-buildx
elif [[ "$COMPONENT" == "reporter" ]]; then
  echo "Building reporter..."
  IMG_TAG=${IMG_TAG} IMG_PREFIX=${IMG_PREFIX}/node-readiness-reporter make docker-buildx-reporter
else
  echo "Unknown component: $COMPONENT"
  exit 1
fi