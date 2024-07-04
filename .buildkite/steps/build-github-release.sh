#!/usr/bin/env bash
set -e

echo '--- Getting agent version from build meta data'

FULL_AGENT_VERSION=$(buildkite-agent meta-data get "agent-version-full")
export FULL_AGENT_VERSION

AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")
export AGENT_VERSION

BUILD_VERSION=$(buildkite-agent meta-data get "agent-version-build")
export BUILD_VERSION

echo "Full agent version: $FULL_AGENT_VERSION"
echo "Agent version: $AGENT_VERSION"
echo "Build version: $BUILD_VERSION"

echo '--- Downloading binaries'

rm -rf pkg
mkdir -p pkg
buildkite-agent artifact download "pkg/*" .

function build() {
  echo "--- Building release for: $1"

  ./scripts/build-github-release.sh "$1" "$AGENT_VERSION"
}

# Export the function so we can use it in xargs
export -f build

# Make sure the releases directory is empty
rm -rf releases

# Loop over all the binaries and build them
find pkg/* -print0 | xargs -I {} bash -c "build {}"
