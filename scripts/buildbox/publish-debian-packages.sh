#!/bin/bash
set -e

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing $CODENAME (`stable` or `unstable`)"
  exit 1
fi

function publish-package {
  echo "--- Publishing $1"
  ./scripts/publish-debian-package.sh $1 $CODENAME
}

# Export the function so we can use it in xargs
export -f publish-package

echo '--- Installing dependencies'
gem install deb-s3
rbenv rehash

echo '--- Downloading package artifacts'
~/.buildbox/bin/buildbox-agent artifact download "pkg/deb/*.deb" . --job ""

# Loop over all the .deb files and publish them
ls pkg/deb/*.deb | xargs publish-package
