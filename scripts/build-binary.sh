#!/usr/bin/env bash
set -e

if [[ ${#} -lt 3 ]]
then
  echo "Usage: ${0} [platform] [arch] [buildVersion]" >&2
  exit 1
fi

export GOOS="$1"
export GOARCH="$2"

BUILD_NUMBER="$3"
NAME="buildkite-agent"

if [[ "${GOOS}" = "dragonflybsd" ]]; then
  export GOOS="dragonfly"
fi

BUILD_PATH="pkg"
BINARY_FILENAME="${NAME}-${GOOS}-${GOARCH}"

if [[ "${GOARCH}" = "armhf" ]]; then
  export GOARCH="arm"
  export GOARM="7"
fi

echo -e "Building ${NAME} with:\n"

echo "GOOS=${GOOS}"
echo "GOARCH=${GOARCH}"
if [[ -n "${GOARM}" ]]; then
  echo "GOARM=${GOARM}"
fi
echo "BUILD_NUMBER=${BUILD_NUMBER}"
echo ""

# Add .exe for Windows builds
if [[ "$GOOS" == "windows" ]]; then
  BINARY_FILENAME="${BINARY_FILENAME}.exe"
fi

# Disable CGO completely
export CGO_ENABLED=0

# Generated files
"$(dirname $0)"/generate-acknowledgements.sh

mkdir -p $BUILD_PATH
go build -v -ldflags "-X github.com/buildkite/agent/v3/version.buildNumber=${BUILD_NUMBER}" -o "${BUILD_PATH}/${BINARY_FILENAME}" .

chmod +x "${BUILD_PATH}/${BINARY_FILENAME}"

echo -e "\nDone: \033[33m${BUILD_PATH}/${BINARY_FILENAME}\033[0m 💪"
