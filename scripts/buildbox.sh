#!/bin/bash
set -e

echo '--- go version'
go version

echo '--- install dependencies'
go get github.com/tools/godep
godep get

echo '--- building packages'
./scripts/build.sh
