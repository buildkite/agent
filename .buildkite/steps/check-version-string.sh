#!/usr/bin/env bash

set -euo pipefail

echo ~~~ Determining architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH=amd64;;
  aarch64) ARCH=arm64;;
  *)
    echo ^^^ +++
    echo "Unknown architecture $ARCH."
    exit 1
    ;;
esac

echo ~~~ Downloading built buildkite-agent binary
buildkite-agent artifact download "pkg/buildkite-agent-linux-$ARCH" .
chmod +x "pkg/buildkite-agent-linux-$ARCH"

echo ~~~ Testing version string is clean
VERSION=$("pkg/buildkite-agent-linux-$ARCH" --version)
echo $ buildkite-agent --verison
echo "$VERSION"
if [[ $VERSION =~ dirty ]]; then
  echo ^^^ +++
  echo Version string is not clean.
  exit 1
fi
