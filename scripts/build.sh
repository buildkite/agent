#!/bin/bash
set -e
set -x

mkdir -p pkg

function build {
  FILENAME=buildbox-agent-$1-$2
  GOOS=$1 GOARCH=$2 go build -o pkg/$FILENAME
  gzip pkg/$FILENAME
}

build "linux" "amd64"
build "linux" "386"
build "darwin" "386"
build "darwin" "amd64"
