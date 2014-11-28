#!/bin/bash
set -e

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing $CODENAME (`stable` or `unstable`)"
  exit 1
fi

function build-package {
  echo "--- Building debian package $1/$2"

  # Attach the buildbox build number to debian packages we're releasing to the
  # unstable chanel.
  if [ "$CODENAME" == "unstable" ]; then
    ./scripts/build-debian-package.sh $1 $2 $BUILDBOX_BUILD_NUMBER
  else
    ./scripts/build-debian-package.sh $1 $2
  fi
}

echo '--- Installing dependencies'
gem install fpm
rbenv rehash

# Build the packages
build-package "linux" "amd64"
build-package "linux" "i386"
