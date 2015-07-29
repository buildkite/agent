#!/bin/bash
set -e

echo '--- Setting up GOPATH'
export GOPATH="$GOPATH:$(pwd)/vendor"
echo $PATH

echo '--- Running golint'
make lint

echo '--- Running tests'
make test
