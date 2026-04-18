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

GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.1.4}"

# Install govulncheck if not already present.
if ! command -v govulncheck &>/dev/null; then
  echo "Installing govulncheck@${GOVULNCHECK_VERSION}..."
  go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
fi

# NRC_VERIFY_GIT_BRANCH is populated in verify CI jobs (e.g. GITHUB_BASE_REF).
BRANCH="${NRC_VERIFY_GIT_BRANCH:-main}"

# Create a temp directory and clean it up on exit.
TMPDIR="$(mktemp -d)"
trap 'rm -rf "${TMPDIR}"' EXIT

WORKTREE="${TMPDIR}/worktree"

echo "Creating worktree for base branch '${BRANCH}'..."
git worktree add -f -q "${WORKTREE}" "${BRANCH}"
trap 'git worktree remove -f "${WORKTREE}"; rm -rf "${TMPDIR}"' EXIT

echo "Running govulncheck on HEAD (PR branch)..."
govulncheck -scan package ./... > "${TMPDIR}/head.txt" || true

echo "Running govulncheck on base branch '${BRANCH}'..."
pushd "${WORKTREE}" >/dev/null
  govulncheck -scan package ./... > "${TMPDIR}/pr-base.txt" || true
popd >/dev/null

echo -e "\n=== HEAD (PR branch) ===\n$(cat "${TMPDIR}/head.txt")"
echo -e "\n=== BASE (${BRANCH}) ===\n$(cat "${TMPDIR}/pr-base.txt")"

diff -s -u --ignore-all-space "${TMPDIR}/pr-base.txt" "${TMPDIR}/head.txt" || true
