#!/usr/bin/env bash
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

IMG_TAG=${IMG_TAG} IMG_PREFIX=${IMG_PREFIX%/} make docker-build

IMG_TAG=${IMG_TAG} IMG_PREFIX=${IMG_PREFIX%/} make docker-push
