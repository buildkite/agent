#!/usr/bin/env bash
set -euo pipefail

if [[ ${#} -lt 3 ]]; then
  echo "Usage: ${0} [arch] [binary] [version] [revision]" >&2
  exit 1
fi

function info {
  echo -e "\033[35m$1\033[0m"
}

BUILD_ARCH="${1}"
BUILD_BINARY_PATH="${2}"
VERSION="${3}"
REVISION="${4}"

NAME="buildkite-agent"
MAINTAINER="dev@buildkite.com"
URL="https://buildkite.com/agent"
DESCRIPTION="The Buildkite Agent is an open-source toolkit written in Golang for securely running build jobs on any device or network"
LICENCE="MIT"
VENDOR="Buildkite"

if [ "$BUILD_ARCH" == "amd64" ]; then
  ARCH="x86_64"
elif [ "$BUILD_ARCH" == "386" ]; then
  ARCH="i386"
elif [ "$BUILD_ARCH" == "arm" ]; then
  ARCH="arm"
elif [ "$BUILD_ARCH" == "armhf" ]; then
  ARCH="armhf"
elif [ "$BUILD_ARCH" == "arm64" ]; then
  ARCH="arm64"
elif [ "$BUILD_ARCH" == "ppc64" ]; then
  ARCH="ppc64"
elif [ "$BUILD_ARCH" == "ppc64le" ]; then
  ARCH="ppc64el"
elif [ "$BUILD_ARCH" == "riscv64" ]; then
  ARCH="riscv64"
else
  echo "Unknown architecture: $BUILD_ARCH"
  exit 1
fi

DESTINATION_PATH="deb"

PACKAGE_NAME="${NAME}_${VERSION}-${REVISION}_${ARCH}.deb"
PACKAGE_PATH="${DESTINATION_PATH}/${PACKAGE_NAME}"

mkdir -p "$DESTINATION_PATH"

info "Installing dependencies"

bundle check || bundle

info "Building debian package $PACKAGE_NAME to $DESTINATION_PATH"

bundle exec fpm -s "dir" \
  -t "deb" \
  -n "$NAME" \
  --url "$URL" \
  --maintainer "$MAINTAINER" \
  --architecture "$ARCH" \
  --license "$LICENCE" \
  --description "$DESCRIPTION" \
  --vendor "$VENDOR" \
  --depends "git-core" \
  --verbose \
  --debug \
  --force \
  --category admin \
  --deb-priority optional \
  --deb-compression bzip2 \
  --before-install "packaging/linux/scripts/before-install.sh" \
  --after-install "packaging/linux/scripts/after-install-and-upgrade.sh" \
  --before-remove "packaging/linux/scripts/before-remove.sh" \
  --after-remove "packaging/linux/scripts/after-remove.sh" \
  --before-upgrade "packaging/linux/scripts/before-upgrade.sh" \
  --after-upgrade "packaging/linux/scripts/after-install-and-upgrade.sh" \
  -p "$PACKAGE_PATH" \
  -v "$VERSION" \
  --iteration "$REVISION" \
  "./$BUILD_BINARY_PATH=/usr/bin/buildkite-agent" \
  "packaging/linux/root/=/"

echo ""
echo -e "Successfully created \033[33m$PACKAGE_PATH\033[0m 👏"
echo ""
echo "    # To install this package"
echo "    $ sudo dpkg -i $PACKAGE_PATH"
echo ""
echo "    # To uninstall"
echo "    $ sudo dpkg --purge $NAME"
echo ""
