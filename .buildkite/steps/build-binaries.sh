#!/bin/bash
set -euo pipefail

GO111MODULE=off go get github.com/mitchellh/gox

# Disable CGO completely
export CGO_ENABLED=0

# mkdir -p $BUILD_PATH
# go build -v -ldflags "-X github.com/buildkite/agent/agent.buildVersion=$BUILD_VERSION" -o $BUILD_PATH/$BINARY_FILENAME *.go

archs=(
  windows/386
  windows/amd64
  linux/amd64
  linux/386
  linux/arm
  linux/armhf
  linux/arm64
  linux/ppc64le
  darwin/386
  darwin/amd64
  freebsd/amd64
  freebsd/386
  openbsd/amd64
  openbsd/386
  dragonfly/amd64
)

rm -rf pkg
gox -parallel=5 -output "pkg/buildkite-agent-{{.OS}}-{{.Arch}}" -osarch "${archs[*]}" .


