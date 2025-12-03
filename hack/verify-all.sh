#!/usr/bin/env bash
set -o errexit
set -o nounset
set -o pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")"/.. && pwd)
"${ROOT_DIR}/hack/verify-boilerplate.sh"
