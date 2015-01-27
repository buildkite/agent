#!/bin/bash
set -e

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing \$CODENAME (stable or unstable)"
  exit 1
fi

function build() {
  echo "--- Building debian package $1/$2"

  BINARY_FILENAME="pkg/buildkite-agent-$1-$2"

  # Download the built binary artifact
  buildbox-agent artifact download $BINARY_FILENAME . --job ""

  # Make sure it's got execute permissions so we can extract the version out of it
  chmod +x $BINARY_FILENAME

  # Build the debian package using the architectre and binary, they are saved to deb/
  ./scripts/utils/build-debian-package.sh $2 $BINARY_FILENAME deb
}

function publish() {
  echo "+++ Shipping $1"

  ./scripts/utils/publish-debian-package.sh $1 $CODENAME
}

# Export the function so we can use it in xargs
export -f publish

echo '--- Installing dependencies'
bundle

# Make sure we have a clean deb folder
rm -rf deb

# Build the packages into deb/
build "linux" "amd64"
build "linux" "386"

# Loop over all the .deb files and publish them
ls deb/*.deb | xargs -I {} bash -c "publish {}"
