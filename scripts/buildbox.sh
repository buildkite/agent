#!/bin/bash
set -e

export GOPATH="`pwd`:$GOPATH"

echo '--- go version'
go version

echo '--- install dependencies'
go get github.com/tools/godep
godep restore

echo '--- building packages'
./scripts/build.sh
