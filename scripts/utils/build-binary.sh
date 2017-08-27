#!/bin/bash
set -e

if [[ ${#} -lt 3 ]]
then
  echo "Usage: ${0} [platform] [arch] [buildVersion]" >&2
  exit 1
fi

export GOOS=${1}
export GOARCH=${2}

BUILD_VERSION=${3}
NAME="buildkite-agent"

BUILD_PATH="pkg"
BINARY_FILENAME="$NAME-$GOOS-$GOARCH"

if [[ "$GOARCH" = "armhf" ]]; then
  export GOARCH="arm"
  export GOARM="7"
fi

echo -e "Building $NAME with:\n"

echo "GOOS=$GOOS"
echo "GOARCH=$GOARCH"
if [[ -n "$GOARM" ]]; then
  echo "GOARM=$GOARM"
fi
echo "BUILD_VERSION=$BUILD_VERSION"
echo ""

# Add .exe for Windows builds
if [[ "$GOOS" == "windows" ]]; then
  BINARY_FILENAME="$BINARY_FILENAME.exe"
fi

# Disable CGO completely
export CGO_ENABLED=0

mkdir -p $BUILD_PATH
go build -v -ldflags "-X github.com/buildkite/agent/agent.buildVersion=$BUILD_VERSION" -o $BUILD_PATH/$BINARY_FILENAME *.go

chmod +x $BUILD_PATH/$BINARY_FILENAME

echo -e "\nDone: \033[33m$BUILD_PATH/$BINARY_FILENAME\033[0m 💪"
