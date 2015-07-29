#!/bin/bash
set -e

echo '--- Setting up GOPATH'
export GOPATH="$GOPATH:$(pwd)/vendor"
echo $GOPATH

echo '--- Running tests'
go list ./... | sed '/vendor/d' | PATH=$TEMPDIR:$PATH xargs -n1 go test
