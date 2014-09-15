#!/bin/bash
set -e
set -x

DIRECTORY=pkg
if [ -d "$DIRECTORY" ]; then
  rm -rf "$DIRECTORY"
fi
mkdir -p "$DIRECTORY"

function build {
  # Build the agent binary
  BINARY_FILENAME=buildbox-agent
  GOOS=$1 GOARCH=$2 go build -o $DIRECTORY/$BINARY_FILENAME *.go

  FILENAME=buildbox-agent-$1-$2

  # Tar up the binaries
  cd $DIRECTORY
  tar cfvz $FILENAME.tar.gz $BINARY_FILENAME
  cd ..

  # Cleanup after the build
  rm $DIRECTORY/$BINARY_FILENAME
}

build "linux" "amd64"
build "linux" "386"
build "linux" "arm"
build "darwin" "386"
build "darwin" "amd64"
