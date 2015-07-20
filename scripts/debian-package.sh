#!/bin/bash
set -e

echo $PATH
whoami
env

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing \$CODENAME (stable or unstable)"
  exit 1
fi

function build() {
  echo "--- Building debian package $1/$2"

  BINARY_FILENAME="pkg/buildkite-agent-$1-$2"

  # Download the built binary artifact
  buildkite-agent artifact download $BINARY_FILENAME .

  # Make sure it's got execute permissions so we can extract the version out of it
  chmod +x $BINARY_FILENAME

  # Build the debian package using the architectre and binary, they are saved to deb/
  ./scripts/utils/build-linux-package.sh "deb" $2 $BINARY_FILENAME
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
build "linux" "arm"

# Loop over all the .deb files and publish them
ls deb/*.deb | xargs -I {} bash -c "publish {}"
