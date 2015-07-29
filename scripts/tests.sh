#!/bin/bash
set -e

echo '--- Setting up GOPATH'
export GOPATH="$(pwd)/vendor:$GOPATH"

echo '--- Running golint'
make lint

echo '--- Running tests'
make test
