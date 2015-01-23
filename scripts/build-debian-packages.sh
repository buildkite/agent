#!/bin/bash
set -e

function build-package {
  echo "--- Building debian package $1/$2"

  BINARY_FILENAME="buildkite-binary-$1-$2"

  # Download the built binary artifact
  buildbox-agent build-artifact download $BINARY_FILENAME .

  # Build the debian package using the architectre and binary
  ./scripts/utils/build-debian-package.sh $2 $BINARY_FILENAME
}

echo '--- Installing dependencies'
bundle --path vendor/bundle
godep restore

# Build the packages
build-package "linux" "amd64"
build-package "linux" "386"
