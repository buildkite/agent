#!/usr/bin/env bash

set -euo pipefail

export GOMODCACHE="$HOME/.gomodcache"
export AGENT_GO_VERSION="$(go env GOVERSION | cut -d. -f1,2)"
export GOCACHE="$HOME/.gocache-target"

# Cache compiled objects by matrix target. TARGET_GOARCH intentionally keeps
# the matrix's "armhf" label so the arm and armhf jobs never compete to save
# the same cache key; scripts/build-binary.sh normalizes it to GOARCH=arm and
# GOARM=7 for the actual build.
export TARGET_GOOS="$1"
export TARGET_GOARCH="$2"
export TARGET_GOARM="none"
export TARGET_BUILD_MODE="cgo-disabled"
export TARGET_GO_VERSION="$(go env GOVERSION)"

case "$2" in
  arm)
    export TARGET_GOARM="default"
    ;;
  armhf)
    export TARGET_GOARM="7"
    ;;
esac

# Restore the shared module cache, this target's compiled objects, and the
# generated attributions file, which generate-acknowledgements.sh reuses rather
# than regenerating.
#
# The module cache restored here is the complete build list, sized by the
# attributions walk rather than by the build itself: `go build` for one target
# needs only that target's imports. Sources have to be present for Go to hash
# them, but they need not be the whole graph.
echo --- :inbox_tray: Restoring Go caches
buildkite-agent cache restore \
  --name gomodcache \
  --name target_gocache \
  --name acknowledgements

export ACKNOWLEDGEMENTS_REUSE_EXISTING=true

echo "--- :${1}: Building ${1}/${2}"

rm -rf pkg

./scripts/build-binary.sh "${1}" "${2}" "${BUILDKITE_BUILD_NUMBER}"

# Each matrix target has exactly one job, so each cache key has one writer.
echo --- :outbox_tray: Saving Go caches
buildkite-agent cache save --name target_gocache --name acknowledgements
