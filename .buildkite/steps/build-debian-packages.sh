#!/usr/bin/env bash
set -euo pipefail

echo "--- Getting agent version from build meta data"

FULL_AGENT_VERSION=$(buildkite-agent meta-data get "agent-version-full")
AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")
BUILD_VERSION=$(buildkite-agent meta-data get "agent-version-build")

echo "Full agent version: $FULL_AGENT_VERSION"
echo "Agent version: $AGENT_VERSION"
echo "Build version: $BUILD_VERSION"

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

# Make sure we have a clean deb folder
rm -rf deb

# Build the packages into deb/
PLATFORM="linux"
for ARCH in "amd64" "386" "arm" "armhf" "arm64" "ppc64" "ppc64le" "riscv64"; do
  echo "--- Building debian package ${PLATFORM}/${ARCH}"

  BINARY="pkg/buildkite-agent-${PLATFORM}-${ARCH}"

  # Download the built binary artifact
  buildkite-agent artifact download "$BINARY" .

  # Make sure it's got execute permissions so we can extract the version out of it
  chmod +x "$BINARY"

  # Build the debian package using the architectre and binary, they are saved to deb/
  ./scripts/ruby-env ./scripts/build-debian-package.sh "$ARCH" "$BINARY" "$AGENT_VERSION" "$BUILD_VERSION"
done
