#!/bin/bash
set -e

echo '--- Building Github packages'
./scripts/build-release.sh "windows" "386"
./scripts/build-release.sh "windows" "amd64"
./scripts/build-release.sh "linux" "amd64"
./scripts/build-release.sh "linux" "386"
./scripts/build-release.sh "linux" "arm"
./scripts/build-release.sh "darwin" "386"
./scripts/build-release.sh "darwin" "amd64"

echo '--- Downloading binaries'
rm -rf pkg
mkdir -p pkg
buildbox-agent artifact download "pkg/*" pkg

echo '--- release'
ruby scripts/publish_release.rb
