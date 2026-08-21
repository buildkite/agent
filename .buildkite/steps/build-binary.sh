#!/usr/bin/env bash

set -euo pipefail

# $HOME-anchored, matching cache.yml's ~-anchored target_paths.
export GOMODCACHE="$HOME/.gomodcache"

# gomodcache ONLY. We deliberately do NOT restore gocache: Go keys compiled objects by
# the *target* GOOS/GOARCH, so the linux/amd64 gocache holds nothing a
# cross-build can reuse.
echo --- :inbox_tray: cache restore
buildkite-agent cache restore --name gomodcache

echo "--- :${1}: Building ${1}/${2}"

rm -rf pkg

./scripts/build-binary.sh "${1}" "${2}" "${BUILDKITE_BUILD_NUMBER}"
