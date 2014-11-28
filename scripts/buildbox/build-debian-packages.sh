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

echo '--- Installing fpm'
gem install fpm
rbenv rehash

echo '--- Installing go dependencies'
# setup the current repo as a package - super hax.
mkdir -p gopath/src/github.com/buildbox
ln -s `pwd` gopath/src/github.com/buildbox/agent
export GOPATH="$GOPATH:`pwd`/gopath"

go get github.com/tools/godep
godep restore

# Build the packages
build-package "linux" "amd64"
build-package "linux" "i386"
