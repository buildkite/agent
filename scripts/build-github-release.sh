#!/bin/bash
set -e

echo '--- Getting agent version from build meta data'

export FULL_AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")
export AGENT_VERSION=$(echo $FULL_AGENT_VERSION | sed 's/buildkite-agent version //')

echo "Full agent version: $FULL_AGENT_VERSION"
echo "Agent version: $AGENT_VERSION"

echo '--- Downloading binaries'

rm -rf pkg
mkdir -p pkg
buildkite-agent artifact download "pkg/*" .

function build() {
  echo "--- Building release for: $1"

  ./scripts/utils/build-github-release.sh $1 $AGENT_VERSION
}

# Export the function so we can use it in xargs
export -f build

# Make sure the releases directory is empty
rm -rf releases

# Loop over all the binaries and build them
ls pkg/* | xargs -I {} bash -c "build {}"
