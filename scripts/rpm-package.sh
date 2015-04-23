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
  echo "--- Building rpm package $1/$2"

  BINARY_FILENAME="pkg/buildkite-agent-$1-$2"

  # Download the built binary artifact
  buildkite-agent artifact download $BINARY_FILENAME . --job ""

  # Make sure it's got execute permissions so we can extract the version out of it
  chmod +x $BINARY_FILENAME

  # Build the rpm package using the architectre and binary, they are saved to rpm/
  ./scripts/utils/build-linux-package.sh "rpm" $2 $BINARY_FILENAME
}

function publish() {
  echo "+++ Shipping $1"

  ./scripts/utils/publish-rpm-package.sh $1 $CODENAME
}

# Export the function so we can use it in xargs
export -f publish

echo '--- Installing dependencies'
bundle

# Make sure we have a clean rpm folder
rm -rf rpm

# Build the packages into rpm/
build "linux" "amd64"
build "linux" "386"

# Loop over all the .rpm files and publish them
# ls rpm/*.rpm | xargs -I {} bash -c "publish {}"
