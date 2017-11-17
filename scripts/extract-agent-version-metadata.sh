#!/bin/bash
set -euo pipefail

agent_version=$(awk -F\" '/var baseVersion string = "/ {print $2}' agent/version.go)
build_version=${BUILDKITE_BUILD_NUMBER:-1}
full_agent_version="buildkite-agent version ${agent_version}, build ${build_version}"

echo "Full agent version: $full_agent_version"
echo "Agent version: $agent_version"
echo "Build version: $build_version"

buildkite-agent meta-data set "agent-version" "$agent_version"
buildkite-agent meta-data set "agent-version-full" "$full_agent_version"
buildkite-agent meta-data set "agent-version-build" "$build_version"
