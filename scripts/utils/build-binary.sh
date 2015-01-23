#!/bin/bash
set -e

if [[ ${#} -lt 3 ]]
then
  echo "Usage: ${0} [platform] [arch] [buildVersion]" >&2
  exit 1
fi

GOOS=${1}
GOARCH=${2}
BUILD_VERSION=${3}
NAME="buildkite-agent"

BUILD_PATH="pkg"
BINARY_NAME="$NAME-$GOOS-$GOARCH"

echo "Building $NAME with:\n"

echo "GOOS=$GOOS"
echo "GOARCH=$GOARCH"
echo "BUILD_VERSION=$BUILD_VERSION"

mkdir -p $BUILD_PATH
go build -ldflags "-X github.com/buildkite/agent/buildkite.buildVersion $BUILD_VERSION" -o $BUILD_PATH/$BINARY_NAME *.go

chmod +x $BUILD_PATH/$BINARY_NAME

echo -e "\nDone: \033[33m$BUILD_PATH/$BINARY_NAME\033[0m 💪"
