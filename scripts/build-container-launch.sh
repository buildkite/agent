#!/usr/bin/env bash
set -euo pipefail

arch="${1:?usage: build-container-launch.sh ARCH OUTPUT}"
output="${2:?usage: build-container-launch.sh ARCH OUTPUT}"
case "$arch" in
  amd64|arm64) ;;
  *) printf 'Unsupported container architecture: %s\n' "$arch" >&2; exit 1 ;;
esac

flags=$(GOOS="$(go env GOHOSTOS)" GOARCH="$(go env GOHOSTARCH)" go run ./packaging/docker/container-launch/flags)
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build \
  -ldflags "-X 'main.startFlags=$flags'" \
  -o "$output" ./packaging/docker/container-launch
