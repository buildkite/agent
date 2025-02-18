#!/usr/bin/env bash
set -euo pipefail

echo "--- Getting agent version from build meta data"

export FULL_AGENT_VERSION=$(buildkite-agent meta-data get "agent-version-full")
export AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")
export BUILD_VERSION=$(buildkite-agent meta-data get "agent-version-build")

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

# Make sure we have a clean rpm folder
rm -rf rpm

# Build the packages into rpm/
PLATFORM="linux"
for ARCH in "amd64" "386" "arm64" "ppc64" "ppc64le" "riscv64"; do
  echo "--- Building rpm package ${PLATFORM}/${ARCH}"

  BINARY="pkg/buildkite-agent-${PLATFORM}-${ARCH}"

  # Download the built binary artifact
  buildkite-agent artifact download "$BINARY" .

  # Make sure it's got execute permissions so we can extract the version out of it
  chmod +x "$BINARY"

  # Build the rpm package using the architecture and binary, they are saved to rpm/
  ./scripts/ruby-env ./scripts/build-rpm-package.sh "$ARCH" "$BINARY" "$AGENT_VERSION" "$BUILD_VERSION"
done
