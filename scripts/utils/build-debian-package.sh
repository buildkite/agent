#!/bin/bash
set -e

if [[ ${#} -lt 2 ]]
then
  echo "Usage: ${0} [arch] [binary]" >&2
  exit 1
fi

function info {
  echo -e "\033[35m$1\033[0m"
}

BUILD_ARCH=${1}
BUILD_BINARY_PATH=${2}

DEB_NAME="buildkite-agent"
DEB_MAINTAINER="<dev@buildkite.com>"
DEB_URL="https://buildkite.com/agent"
DEB_DESCRIPTION="The Buildkite Agent is an open-source toolkit written in Golang for securely running build jobs on any device or network"
DEB_LICENCE="MIT"

# Grab the version from the binary. The version spits out as: buildkite-agent
# version 1.0-beta.6 We cut out the 'buildkite-agent version ' part of it.
DEB_VERSION=$($BUILD_BINARY_PATH --version | sed 's/buildkite-agent version //')

if [ "$BUILD_ARCH" == "amd64" ]; then
  DEB_ARCH="x86_64"
elif [ "$BUILD_ARCH" == "386" ]; then
  DEB_ARCH="i386"
else
  echo "Unknown architecture: $BUILD_ARCH"
  exit 1
fi

PACKAGE_NAME=$DEB_NAME"_"$DEB_VERSION"_"$DEB_ARCH".deb"
PACKAGE_PATH="deb/$PACKAGE_NAME"

# Ensure a clean package path
rm -rf "$PACKAGE_PATH"
mkdir -p "$PACKAGE_PATH"

info "Building debian package $PACKAGE_NAME"

bundle exec fpm -s "dir" \
  -t "deb" \
  -n "$DEB_NAME" \
  --url "$DEB_URL" \
  --maintainer "$DEB_MAINTAINER" \
  --architecture "$DEB_ARCH" \
  --license "$DEB_LICENCE" \
  --description "$DEB_DESCRIPTION" \
  --depends "git-core" \
  --config-files "/etc/buildkite-agent/buildkite-agent.env" \
  --config-files "/etc/buildkite-agent/bootstrap.sh" \
  --before-remove "templates/apt-package/before-remove.sh" \
  --after-upgrade "templates/apt-package/after-upgrade.sh" \
  --deb-upstart "templates/apt-package/buildkite-agent.upstart" \
  -p "$PACKAGE_PATH" \
  -v "$DEB_VERSION" \
  "./$BUILD_BINARY_PATH=/usr/bin/buildkite-agent" \
  "templates/apt-package/buildkite-agent.env=/etc/buildkite-agent/buildkite-agent.env" \
  "templates/bootstrap.sh=/etc/buildkite-agent/bootstrap.sh"

echo ""
echo -e "Successfully created \033[33m$PACKAGE_PATH\033[0m 👏"
echo ""
echo "    # To install this package"
echo "    $ sudo dpkg -i $PACKAGE_PATH"
echo ""
echo "    # To uninstall"
echo "    $ sudo dpkg --purge $DEB_NAME"
echo ""
