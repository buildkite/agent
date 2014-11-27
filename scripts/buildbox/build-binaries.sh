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

echo '--- installing github-release'
go get github.com/buildbox/github-release

echo '--- building'
./scripts/build.sh
