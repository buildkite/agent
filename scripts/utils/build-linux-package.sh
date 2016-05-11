#!/bin/bash
set -e

if [[ ${#} -lt 4 ]]
then
  echo "Usage: ${0} [type] [arch] [binary] [version] [revision]" >&2
  exit 1
fi

function info {
  echo -e "\033[35m$1\033[0m"
}

PACKAGE_TYPE=${1}
BUILD_ARCH=${2}
BUILD_BINARY_PATH=${3}
VERSION=${4}
REVISION=${5}

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
else
  echo "Unknown architecture: $BUILD_ARCH"
  exit 1
fi

DESTINATION_PATH="$PACKAGE_TYPE"

PACKAGE_NAME=$NAME"_"$VERSION"-"$REVISION"_"$ARCH".$PACKAGE_TYPE"
PACKAGE_PATH="$DESTINATION_PATH/$PACKAGE_NAME"

mkdir -p $DESTINATION_PATH

info "Building $PACKAGE_TYPE package $PACKAGE_NAME to $DESTINATION_PATH"

bundle exec fpm -s "dir" \
  -t "$PACKAGE_TYPE" \
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
  --rpm-compression bzip2 \
  --rpm-os linux \
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

if [ "$PACKAGE_TYPE" == "deb" ]; then
  echo "    # To install this package"
  echo "    $ sudo dpkg -i $PACKAGE_PATH"
  echo ""
  echo "    # To uninstall"
  echo "    $ sudo dpkg --purge $NAME"
elif [ "$PACKAGE_TYPE" == "rpm" ]; then
  echo "    # To install this package"
  echo "    $ sudo rpm -i $PACKAGE_PATH"
  echo ""
  echo "    # To uninstall"
  echo "    $ sudo rpm -ev $NAME"
fi

echo ""
