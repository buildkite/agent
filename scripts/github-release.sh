#!/bin/bash
set -e

if [[ "$GITHUB_RELEASE_ACCESS_TOKEN" == "" ]]; then
  echo "Error: Missing \$GITHUB_RELEASE_ACCESS_TOKEN"
  exit 1
fi

echo '--- Downloading binaries'

rm -rf pkg
mkdir -p pkg
buildbox-agent artifact download "pkg/*" .

function build() {
  echo "--- Building release for: $1"
}

# Export the function so we can use it in xargs
export -f build

# Loop over all the .deb files and build them
ls pkg/* | xargs -I {} bash -c "build {}"

echo '--- Getting agent version from build meta data'

FULL_AGENT_VERSION=$(buildbox-agent build-data get "agent-version")

SHORT_AGENT_VERSION=$($FULL_AGENT_VERSION | sed 's/buildkite-agent version //')

echo "--- 🚀 $SHORT_AGENT_VERSION"

if [[ "$AGENT_VERSION" == *"beta"* || "$AGENT_VERSION" == *"alpha"* ]]; then
  export GITHUB_RELEASE_PRERELEASE="true"
fi

export GITHUB_RELEASE_REPOSITORY="buildbox/agent"

github-release $SHORT_AGENT_VERSION pkg/*.tar.gz
