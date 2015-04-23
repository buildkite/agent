#!/bin/bash
set -e

if [[ ${#} -lt 3 ]]
then
  echo "Usage: ${0} [type] [arch] [binary]" >&2
  exit 1
fi

function info {
  echo -e "\033[35m$1\033[0m"
}

PACKAGE_TYPE=${1}
BUILD_ARCH=${2}
BUILD_BINARY_PATH=${3}

NAME="buildkite-agent"
MAINTAINER="dev@buildkite.com"
URL="https://buildkite.com/agent"
DESCRIPTION="The Buildkite Agent is an open-source toolkit written in Golang for securely running build jobs on any device or network"
LICENCE="MIT"
VENDOR="Buildkite"

# Grab the version from the binary. The version spits out as: buildkite-agent
# version 1.0-beta.6 We cut out the 'buildkite-agent version ' part of it.
VERSION=$($BUILD_BINARY_PATH --version | sed 's/buildkite-agent version //')

if [ "$BUILD_ARCH" == "amd64" ]; then
  ARCH="x86_64"
elif [ "$BUILD_ARCH" == "386" ]; then
  ARCH="i386"
else
  echo "Unknown architecture: $BUILD_ARCH"
  exit 1
fi

DESTINATION_PATH="$PACKAGE_TYPE"

PACKAGE_NAME=$NAME"_"$VERSION"_"$ARCH".$PACKAGE_TYPE"
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
  --config-files "/etc/buildkite-agent/buildkite-agent.env" \
  --config-files "/etc/buildkite-agent/buildkite-agent.cfg" \
  --config-files "/etc/buildkite-agent/bootstrap.sh" \
  --before-install "templates/linux-package/before-install.sh" \
  --after-install "templates/linux-package/after-install.sh" \
  --before-remove "templates/linux-package/before-remove.sh" \
  --after-remove "templates/linux-package/after-remove.sh" \
  --before-upgrade "templates/linux-package/before-upgrade.sh" \
  --after-upgrade "templates/linux-package/after-upgrade.sh" \
  --deb-upstart "templates/linux-package/buildkite-agent.upstart" \
  --rpm-init "templates/linux-package/buildkite-agent.init" \
  -p "$PACKAGE_PATH" \
  -v "$VERSION" \
  "./$BUILD_BINARY_PATH=/usr/bin/buildkite-agent" \
  "templates/linux-package/buildkite-agent.env=/etc/buildkite-agent/buildkite-agent.env" \
  "templates/linux-package/buildkite-agent.cfg=/etc/buildkite-agent/buildkite-agent.cfg" \
  "templates/bootstrap.sh=/etc/buildkite-agent/bootstrap.sh" \
  "templates/hooks-unix/environment.sample=/etc/buildkite-agent/hooks/environment.sample" \
  "templates/hooks-unix/checkout.sample=/etc/buildkite-agent/hooks/checkout.sample" \
  "templates/hooks-unix/command.sample=/etc/buildkite-agent/hooks/command.sample" \
  "templates/hooks-unix/post-checkout.sample=/etc/buildkite-agent/hooks/post-checkout.sample" \
  "templates/hooks-unix/pre-checkout.sample=/etc/buildkite-agent/hooks/pre-checkout.sample" \
  "templates/hooks-unix/post-command.sample=/etc/buildkite-agent/hooks/post-command.sample" \
  "templates/hooks-unix/pre-command.sample=/etc/buildkite-agent/hooks/pre-command.sample"

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
