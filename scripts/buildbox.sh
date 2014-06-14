#!/bin/bash
set -e

echo '--- go version'
go version

echo '--- install dependencies'
go get github.com/tools/godep
godep restore

# setup the current repo as a package
mkdir -p gopath/src/github.com/buildboxhq/buildbox-agent
ln -s buildbox gopath/src/github.com/buildboxhq/buildbox-agent/buildbox
export GOPATH="`pwd`/gopath:$GOPATH"

echo '--- building packages'
./scripts/build.sh
