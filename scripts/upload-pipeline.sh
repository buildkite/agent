#!/bin/bash
set -euo pipefail

echo '+++ Extracting agent version source code'

export AGENT_VERSION=$(awk -F\" '/var baseVersion string = "/ {print $2}' agent/version.go)
export BUILD_VERSION=$BUILDKITE_BUILD_NUMBER
export FULL_AGENT_VERSION="buildkite-agent version ${AGENT_VERSION}, build ${BUILD_VERSION}"

echo "Full agent version: $FULL_AGENT_VERSION"
echo "Agent version: $AGENT_VERSION"
echo "Build version: $BUILD_VERSION"

buildkite-agent meta-data set "agent-version" "$AGENT_VERSION"
buildkite-agent meta-data set "agent-version-full" "$FULL_AGENT_VERSION"
buildkite-agent meta-data set "agent-version-build" "$BUILD_VERSION"

PIPELINE_FILE=${1:-.buildkite/pipeline.yml}

echo "+++ Uploading pipeline file ${PIPELINE_FILE}"

buildkite-agent pipeline upload "${PIPELINE_FILE}"
