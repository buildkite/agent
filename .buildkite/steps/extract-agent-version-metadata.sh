#!/bin/bash
set -euo pipefail

agent_version=$(awk -F\" '/var baseVersion string = "/ {print $2}' agent/version.go)
build_version=${BUILDKITE_BUILD_NUMBER:-1}
full_agent_version="buildkite-agent version ${agent_version}, build ${build_version}"
docker_alpine_image_tag="buildkiteci/agent:alpine-build-${BUILDKITE_BUILD_NUMBER}"

is_prerelease=0
if [[ "$agent_version" =~ (alpha|beta|rc) ]] ; then
  is_prerelease=1
fi

echo "Full agent version: $full_agent_version"
echo "Agent version: $agent_version"
echo "Build version: $build_version"
echo "Docker Alpine Image Tag: $docker_alpine_image_tag"
echo "Is prerelease? $is_prerelease"

buildkite-agent meta-data set "agent-version" "$agent_version"
buildkite-agent meta-data set "agent-version-full" "$full_agent_version"
buildkite-agent meta-data set "agent-version-build" "$build_version"
buildkite-agent meta-data set "agent-docker-image-alpine" "$docker_alpine_image_tag"
buildkite-agent meta-data set "agent-is-prerelease" "$is_prerelease"
