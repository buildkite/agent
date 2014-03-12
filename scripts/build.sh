#!/bin/bash
set -e
set -x

DIRECTORY=pkg
if [ -d "$DIRECTORY" ]; then
  rm -rf "$DIRECTORY"
fi
mkdir -p "$DIRECTORY"

function build {
  FILENAME=buildbox-agent-$1-$2
  GOOS=$1 GOARCH=$2 go build -o $DIRECTORY/$FILENAME agent.go
  gzip $DIRECTORY/$FILENAME

  FILENAME=buildbox-artifact-$1-$2
  GOOS=$1 GOARCH=$2 go build -o $DIRECTORY/$FILENAME artifact.go
  gzip $DIRECTORY/$FILENAME
}

build "linux" "amd64"
build "linux" "386"
build "darwin" "386"
build "darwin" "amd64"
