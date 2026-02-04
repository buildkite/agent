#!/usr/bin/env bash
set -euo pipefail

agent_version=$(cat version/VERSION)
build_version=${BUILDKITE_BUILD_NUMBER:-1}
full_agent_version="buildkite-agent version ${agent_version}, build ${build_version}"

# docker variants
registry="445615400570.dkr.ecr.us-east-1.amazonaws.com/agent"
docker_alpine_image_tag="$registry:alpine-build-${BUILDKITE_BUILD_NUMBER}"
docker_alpine_k8s_image_tag="$registry:alpine-k8s-build-${BUILDKITE_BUILD_NUMBER}"
docker_ubuntu_focal_image_tag="$registry:ubuntu-20.04-build-${BUILDKITE_BUILD_NUMBER}"
docker_ubuntu_jammy_image_tag="$registry:ubuntu-22.04-build-${BUILDKITE_BUILD_NUMBER}"
docker_ubuntu_noble_image_tag="$registry:ubuntu-24.04-build-${BUILDKITE_BUILD_NUMBER}"

docker_sidecar_image_tag="$registry:sidecar-build-${BUILDKITE_BUILD_NUMBER}"

is_prerelease=0
if [[ "$agent_version" =~ (alpha|beta|rc) ]] ; then
  is_prerelease=1
fi

echo "Full agent version: $full_agent_version"
echo "Agent version: $agent_version"
echo "Build version: $build_version"
echo "Docker Alpine Image Tag: $docker_alpine_image_tag"
echo "Docker Ubuntu 20.04 Image Tag: $docker_ubuntu_focal_image_tag"
echo "Docker Ubuntu 22.04 Image Tag: $docker_ubuntu_jammy_image_tag"
echo "Docker Ubuntu 24.04 Image Tag: $docker_ubuntu_noble_image_tag"
echo "Docker Sidecar Image Tag: $docker_sidecar_image_tag"
echo "Is prerelease? $is_prerelease"

buildkite-agent meta-data set "agent-version" "$agent_version"
buildkite-agent meta-data set "agent-version-full" "$full_agent_version"
buildkite-agent meta-data set "agent-version-build" "$build_version"
buildkite-agent meta-data set "agent-docker-image-alpine" "$docker_alpine_image_tag"
buildkite-agent meta-data set "agent-docker-image-alpine-k8s" "$docker_alpine_k8s_image_tag"
buildkite-agent meta-data set "agent-docker-image-ubuntu-20.04" "$docker_ubuntu_focal_image_tag"
buildkite-agent meta-data set "agent-docker-image-ubuntu-22.04" "$docker_ubuntu_jammy_image_tag"
buildkite-agent meta-data set "agent-docker-image-ubuntu-24.04" "$docker_ubuntu_noble_image_tag"
buildkite-agent meta-data set "agent-docker-image-sidecar" "$docker_sidecar_image_tag"
buildkite-agent meta-data set "agent-is-prerelease" "$is_prerelease"
