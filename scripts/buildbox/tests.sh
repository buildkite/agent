#!/bin/bash
set -e

# setup the current repo as a package - super hax.
mkdir -p gopath/src/github.com/buildbox
ln -s `pwd` gopath/src/github.com/buildbox/agent
export GOPATH="$GOPATH:`pwd`/gopath"

echo '--- go version'
go version

echo '--- install dependencies'
go get github.com/tools/godep
godep restore

echo '--- golint'
go get github.com/golang/lint/golint
make lint

echo '--- tests'
make test
