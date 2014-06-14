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
  AGENT_FILENAME=buildbox-agent
  GOOS=$1 GOARCH=$2 go build -o $DIRECTORY/$AGENT_FILENAME agent.go

  # Build the artifact binary
  ARTIFACT_FILENAME=buildbox-artifact
  GOOS=$1 GOARCH=$2 go build -o $DIRECTORY/$ARTIFACT_FILENAME artifact.go

  # Build the data binary
  DATA_FILENAME=buildbox-data
  GOOS=$1 GOARCH=$2 go build -o $DIRECTORY/$DATA_FILENAME data.go

  FILENAME=buildbox-agent-$1-$2

  # Tar up the binaries
  cd $DIRECTORY
  tar cfvz $FILENAME.tar.gz $AGENT_FILENAME $ARTIFACT_FILENAME $DATA_FILENAME
  cd ..

  # Cleanup after the build
  rm $DIRECTORY/$AGENT_FILENAME $DIRECTORY/$ARTIFACT_FILENAME $DIRECTORY/$DATA_FILENAME
}

build "linux" "amd64"
build "linux" "386"
build "linux" "arm"
build "darwin" "386"
build "darwin" "amd64"
