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

# This script checks for broken links for all markdown files.
# Usage: `hack/verify-links.sh`.

set -o errexit
set -o nounset
set -o pipefail

# Get the root directory
ROOT_DIR=$(dirname "${BASH_SOURCE[0]}")/..

cd "$ROOT_DIR"

# Detect OS and architecture
os=$(go env GOOS)
arch=$(go env GOARCH)

# Map to lychee release asset names
LYCHEE_VERSION="${LYCHEE_VERSION:-0.23.0}"
LYCHEE_BINARY="lychee"
EXT="tar.gz"
EXTRACT_CMD="tar -C /tmp -xzf"
case "${os}-${arch}" in
linux-amd64)
    LYCHEE_BASENAME="${LYCHEE_BINARY}-x86_64-unknown-linux-gnu"
    ;;
linux-arm64)
    LYCHEE_BASENAME="${LYCHEE_BINARY}-aarch64-unknown-linux-gnu"
    ;;
darwin-arm64)
    LYCHEE_BASENAME="${LYCHEE_BINARY}-arm64-macos"
    ;;
windows-amd64)
    echo "skipping verify-links for windows"
    exit 0
    ;;
*)
    echo "Unsupported platform: ${os}-${arch}" >&2
    exit 1
    ;;
esac

LYCHEE_URL="https://github.com/lycheeverse/lychee/releases/download/lychee-v${LYCHEE_VERSION}/${LYCHEE_BASENAME}.${EXT}"

echo "downloading ${LYCHEE_BASENAME}.${EXT} from ${LYCHEE_URL}"
set -x
curl -fL -o /tmp/${LYCHEE_BINARY}.${EXT} "${LYCHEE_URL}"

${EXTRACT_CMD} "/tmp/${LYCHEE_BINARY}.${EXT}"
chmod +x /tmp/${LYCHEE_BINARY}


# Remove artifacts
rm -rf /tmp/${LYCHEE_BINARY}.${EXT}
rm -rf /tmp/docs

# Change back to root directory
cd "$ROOT_DIR"

echo "Checking links in Markdown files..."
"/tmp/${LYCHEE_BINARY}" --no-progress --timeout 30 --format detailed  '**/*.md'

rm -rf /tmp/${LYCHEE_BINARY}
