#!/bin/bash
set -e

echo '--- go version'
go version

# setup the current repo as a package - super hax.
mkdir -p gopath/src/github.com/buildbox
ln -s `pwd` gopath/src/github.com/buildbox/agent
export GOPATH="$GOPATH:`pwd`/gopath"

echo '--- install dependencies'
go get github.com/tools/godep
godep restore

echo '--- building'
./scripts/build-release.sh "windows" "386"
./scripts/build-release.sh "windows" "amd64"
./scripts/build-release.sh "linux" "amd64"
./scripts/build-release.sh "linux" "386"
./scripts/build-release.sh "linux" "arm"
./scripts/build-release.sh "darwin" "386"
./scripts/build-release.sh "darwin" "amd64"
