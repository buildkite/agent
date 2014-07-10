#!/bin/bash
set -e

# setup the current repo as a package - super hax.
mkdir -p gopath/src/github.com/buildboxhq
ln -s `pwd` gopath/src/github.com/buildboxhq/buildbox-agent
export GOPATH="$GOPATH:`pwd`/gopath"

echo '--- install dependencies'
go get github.com/tools/godep
godep restore

echo '--- installing github-release'
go get github.com/buildboxhq/github-release

echo '--- building'
./scripts/build.sh

echo '--- release'
ruby scripts/publish_release.rb
