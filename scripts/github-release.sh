#!/bin/bash
set -e

if [[ "$GITHUB_RELEASE_ACCESS_TOKEN" == "" ]]; then
  echo "Error: Missing \$GITHUB_RELEASE_ACCESS_TOKEN"
  exit 1
fi

echo '--- Downloading binaries'

rm -rf pkg
mkdir -p pkg
# buildbox-agent build-artifact download "pkg/*" .

function build() {
  echo "--- Building release for: $1"

  ./scripts/utils/build-github-release.sh $1
}

# Export the function so we can use it in xargs
export -f build

# Make sure the releases directory is empty
rm -rf releases

# Loop over all the .deb files and build them
ls pkg/* | xargs -I {} bash -c "build {}"

echo '--- Getting agent version from build meta data'

AGENT_VERSION=$(buildbox-agent build-data get "agent-version" | sed 's/buildkite-agent version //')

echo "Version is $AGENT_VERSION"

echo "--- 🚀 $AGENT_VERSION"

export GITHUB_RELEASE_REPOSITORY="buildkite/agent"

if [[ "$AGENT_VERSION" == *"beta"* || "$AGENT_VERSION" == *"alpha"* ]]; then
  # Beta versions of the agent will have the build number at the end of them
  # like this:
  #
  #    buildkite-agent version 1.0-beta.7.227
  #
  # We don't want the build numbers for GitHub releases, so this command will
  # drop them.
  AGENT_VERSION=$($AGENT_VERSION | sed -E 's/\.[0-9]+$//')

  github-release "v$AGENT_VERSION" releases/* --prerelease
else
  github-release "v$AGENT_VERSION" releases/*
fi
