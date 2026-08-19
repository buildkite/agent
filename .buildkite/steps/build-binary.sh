#!/usr/bin/env bash

set -euo pipefail

export GOMODCACHE="$HOME/.gomodcache"
export AGENT_GO_VERSION="$(go env GOVERSION | cut -d. -f1,2)"

# Cross-compilation cannot reuse the host's compiled-object cache, but it can
# reuse the platform-independent module cache.
echo --- :inbox_tray: Restoring Go module cache
buildkite-agent cache restore --name gomodcache

echo "--- :${1}: Building ${1}/${2}"

rm -rf pkg

./scripts/build-binary.sh "${1}" "${2}" "${BUILDKITE_BUILD_NUMBER}"
