#!/usr/bin/env bash

set -euo pipefail

export GOMODCACHE="$HOME/.gomodcache"
export AGENT_GO_VERSION="$(go env GOVERSION | cut -d. -f1,2)"
export GOCACHE="$HOME/.gocache-target"

# Keep armhf in the cache key; the build script normalises it to arm/7.
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

echo --- :inbox_tray: Restoring Go caches
buildkite-agent cache restore \
  --name gomodcache \
  --name target_gocache \
  --name acknowledgements

export ACKNOWLEDGEMENTS_REUSE_EXISTING=true

echo "--- :${1}: Building ${1}/${2}"

rm -rf pkg

./scripts/build-binary.sh "${1}" "${2}" "${BUILDKITE_BUILD_NUMBER}"

if [[ "$1" == linux && ( "$2" == amd64 || "$2" == arm64 ) ]]; then
  mkdir -p tmp
  bash ./scripts/build-container-launch.sh "$2" "tmp/buildkite-container-launch-linux-$2"
  GOOS=linux GOARCH="$2" CGO_ENABLED=0 go test -c -o "tmp/containerimage-test-linux-$2" ./internal/containerimage
fi

echo --- :outbox_tray: Saving Go caches
buildkite-agent cache save --name target_gocache --name acknowledgements
