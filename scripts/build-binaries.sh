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

echo '--- Installing dependencies'
godep restore

# Clear out the pkg directory
rm -rf pkg

build-binary "windows" "386"
build-binary "windows" "amd64"
build-binary "linux" "amd64"
build-binary "linux" "386"
build-binary "linux" "arm"
build-binary "darwin" "386"
build-binary "darwin" "amd64"
