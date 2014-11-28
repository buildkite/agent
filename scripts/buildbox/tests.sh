#!/bin/bash
set -e

# setup the current repo as a package - super hax.
mkdir -p gopath/src/github.com/buildbox
ln -s `pwd` gopath/src/github.com/buildbox/agent
export GOPATH="$GOPATH:`pwd`/gopath"

echo '--- Install dependencies'
go get github.com/tools/godep
godep restore

echo '--- Running golint'
go get github.com/golang/lint/golint
make lint

echo '--- Running tests'
make test
