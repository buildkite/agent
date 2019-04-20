#!/bin/bash
set -euo pipefail

GO111MODULE=off go get github.com/mitchellh/gox

# Disable CGO completely
export CGO_ENABLED=0

ldflags="-X github.com/buildkite/agent/agent.buildVersion=$BUILDKITE_BUILD_NUMBER"

gox_archs=(
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

# Build what we can with gox
gox -ldflags="$ldflags" -parallel=8 -output "pkg/buildkite-agent-{{.OS}}-{{.Arch}}" -osarch "${gox_archs[*]}" .

# Build the rest directly with golang
GOOS="linux" GOARCH="arm" GOARM="7" go build -ldflags "${ldflags}" -o "pkg/buildkite-agent-linux-armhf"
