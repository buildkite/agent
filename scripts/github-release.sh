#!/bin/bash
set -e

if [[ "$GITHUB_RELEASE_ACCESS_TOKEN" == "" ]]; then
  echo "Error: Missing \$GITHUB_RELEASE_ACCESS_TOKEN"
  exit 1
fi

echo '--- Downloading binaries'

rm -rf pkg
mkdir -p pkg
buildbox-agent build-artifact download "pkg/*" .

function build() {
  echo "--- Building release for: $1"
}

# Export the function so we can use it in xargs
export -f build

# Make sure the releases directory is empty
rm -rf releases

# Loop over all the .deb files and build them
ls pkg/* | xargs -I {} bash -c "build {}"

echo '--- Getting agent version from build meta data'

AGENT_VERSION=$(buildbox-agent build-data get "agent-version" | sed 's/buildkite-agent version //')

echo "--- 🚀 $AGENT_VERSION"

if [[ "$AGENT_VERSION" == *"beta"* || "$AGENT_VERSION" == *"alpha"* ]]; then
  export GITHUB_RELEASE_PRERELEASE="true"
fi

export GITHUB_RELEASE_REPOSITORY="buildkite/agent"

github-release "v$AGENT_VERSION" releases/*
