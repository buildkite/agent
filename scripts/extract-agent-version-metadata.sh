#!/bin/bash

set -euo pipefail

BIN_NAME="pkg/buildkite-agent-linux-amd64"

echo '--- Downloading built agent'

mkdir pkg
buildkite-agent artifact download "${BIN_NAME}" pkg
chmod +x "${BIN_NAME}"

echo '+++ Extracting agent version from binary'

FULL_AGENT_VERSION=$("${BIN_NAME}" --version)
AGENT_VERSION=$(echo $FULL_AGENT_VERSION | sed 's/buildkite-agent version //' | sed -E 's/\, build .+//')
BUILD_VERSION=$(echo $FULL_AGENT_VERSION | sed 's/buildkite-agent version .*, build //')

echo "Full agent version: $FULL_AGENT_VERSION"
echo "Agent version: $AGENT_VERSION"
echo "Build version: $BUILD_VERSION"

buildkite-agent meta-data set "agent-version" "$AGENT_VERSION"
buildkite-agent meta-data set "agent-version-full" "$FULL_AGENT_VERSION"
buildkite-agent meta-data set "agent-version-build" "$BUILD_VERSION"