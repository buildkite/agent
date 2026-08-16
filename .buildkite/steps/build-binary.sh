#!/usr/bin/env bash

set -euo pipefail

# $HOME-anchored, matching cache.yml's ~-anchored target_paths.
export GOCACHE="$HOME/.gocache"
export GOMODCACHE="$HOME/.gomodcache"

echo --- :inbox_tray: cache restore
buildkite-agent cache restore --name gomodcache --name gocache

echo "--- :${1}: Building ${1}/${2}"

rm -rf pkg

./scripts/build-binary.sh "${1}" "${2}" "${BUILDKITE_BUILD_NUMBER}"
