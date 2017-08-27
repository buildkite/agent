#!/bin/bash
set -e

if [[ "$GITHUB_RELEASE_ACCESS_TOKEN" == "" ]]; then
  echo "Error: Missing \$GITHUB_RELEASE_ACCESS_TOKEN"
  exit 1
fi

echo '--- Getting agent version from build meta data'

export FULL_AGENT_VERSION=$(buildkite-agent meta-data get "agent-version-full")
export AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")
export BUILD_VERSION=$(buildkite-agent meta-data get "agent-version-build")

echo "Full agent version: $FULL_AGENT_VERSION"
echo "Agent version: $AGENT_VERSION"
echo "Build version: $BUILD_VERSION"

echo '--- Downloading releases'

rm -rf releases
mkdir -p releases
buildkite-agent artifact download "releases/*" .

echo "Version is $FULL_AGENT_VERSION"

export GITHUB_RELEASE_REPOSITORY="buildkite/agent"

if [[ "$AGENT_VERSION" == *"beta"* || "$AGENT_VERSION" == *"alpha"* ]]; then
  echo "--- 🚀 $AGENT_VERSION (prerelease)"

  buildkite-agent meta-data set github_release_type "prerelease"
  buildkite-agent meta-data set github_release_version $AGENT_VERSION

  github-release "v$AGENT_VERSION" releases/* --commit "$(git rev-parse HEAD)" --prerelease
else
  echo "--- 🚀 $AGENT_VERSION"

  buildkite-agent meta-data set github_release_type "stable"
  buildkite-agent meta-data set github_release_version $AGENT_VERSION

  github-release "v$AGENT_VERSION" releases/* --commit "$(git rev-parse HEAD)"
fi
