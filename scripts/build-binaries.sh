#!/bin/bash
set -e

if [[ "$BUILDKITE_BUILD_NUMBER" == "" ]]; then
  echo "Error: Missing \$BUILDKITE_BUILD_NUMBER"
  exit 1
fi

function build-binary {
  echo "--- Building binary for $1/$2"

  ./scripts/utils/build-binary.sh $1 $2 $BUILDKITE_BUILD_NUMBER
}

echo '--- Setting up GOPATH'
export GOPATH="$GOPATH:$(pwd)/vendor"
echo $GOPATH

# Clear out the pkg directory
rm -rf pkg

build-binary "windows" "386"
build-binary "windows" "amd64"
build-binary "linux" "amd64"
build-binary "linux" "386"
build-binary "linux" "arm"
build-binary "linux" "armhf"
build-binary "darwin" "386"
build-binary "darwin" "amd64"
build-binary "freebsd" "amd64"
build-binary "freebsd" "386"

# Grab the version of the binary while we're here (we need it if we deploy this
# commit to GitHub)
echo '--- Saving agent version to build meta data'

FULL_AGENT_VERSION=`pkg/buildkite-agent-linux-386 --version`
AGENT_VERSION=$(echo $FULL_AGENT_VERSION | sed 's/buildkite-agent version //' | sed -E 's/\, build .+//')
BUILD_VERSION=$(echo $FULL_AGENT_VERSION | sed 's/buildkite-agent version .*, build //')

echo "Full agent version: $FULL_AGENT_VERSION"
echo "Agent version: $AGENT_VERSION"
echo "Build version: $BUILD_VERSION"

buildkite-agent meta-data set "agent-version" "$AGENT_VERSION"
buildkite-agent meta-data set "agent-version-full" "$FULL_AGENT_VERSION"
buildkite-agent meta-data set "agent-version-build" "$BUILD_VERSION"
