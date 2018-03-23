#!/bin/bash
set -e

echo '--- Getting agent version from build meta data'

export FULL_AGENT_VERSION; FULL_AGENT_VERSION=$(buildkite-agent meta-data get "agent-version-full")
export AGENT_VERSION; AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")
export BUILD_VERSION; BUILD_VERSION=$(buildkite-agent meta-data get "agent-version-build")

echo "Full agent version: $FULL_AGENT_VERSION"
echo "Agent version: $AGENT_VERSION"
echo "Build version: $BUILD_VERSION"

rm -rf pkg
mkdir -p pkg

echo '--- Downloading :linux: binaries'

buildkite-agent artifact download "pkg/buildkite-agent-linux-amd64" .

image_tag="buildkite-agent-linux-build-${BUILDKITE_BUILD_NUMBER}"

echo '--- Building :linux: :docker: image'

cp pkg/buildkite-agent-linux-amd64  packaging/docker/linux/buildkite-agent
docker build --tag "$image_tag" packaging/docker/linux
