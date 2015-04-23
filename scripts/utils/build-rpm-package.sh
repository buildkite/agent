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
DESTINATION_PATH=${3}

RPM_NAME="buildkite-agent"
RPM_MAINTAINER="dev@buildkite.com"
RPM_URL="https://buildkite.com/agent"
RPM_DESCRIPTION="The Buildkite Agent is an open-source toolkit written in Golang for securely running build jobs on any device or network"
RPM_LICENCE="MIT"
RPM_VENDOR="Buildkite"

# Grab the version from the binary. The version spits out as: buildkite-agent
# version 1.0-beta.6 We cut out the 'buildkite-agent version ' part of it.
RPM_VERSION=$($BUILD_BINARY_PATH --version | sed 's/buildkite-agent version //')

if [ "$BUILD_ARCH" == "amd64" ]; then
  RPM_ARCH="x86_64"
elif [ "$BUILD_ARCH" == "386" ]; then
  RPM_ARCH="i386"
else
  echo "Unknown architecture: $BUILD_ARCH"
  exit 1
fi

PACKAGE_NAME=$RPM_NAME"_"$RPM_VERSION"_"$RPM_ARCH".rpm"
PACKAGE_PATH="$DESTINATION_PATH/$PACKAGE_NAME"

mkdir -p $DESTINATION_PATH

info "Building rpm package $PACKAGE_NAME to $DESTINATION_PATH"

# --config-files "/etc/buildkite-agent/buildkite-agent.env" \
# --config-files "/etc/buildkite-agent/bootstrap.sh" \
# --before-remove "templates/apt-package/before-remove.sh" \
# --after-upgrade "templates/apt-package/after-upgrade.sh" \
# --rpm-init "templates/apt-package/buildkite-agent.upstart" \

bundle exec fpm -s "dir" \
  -t "rpm" \
  -n "$RPM_NAME" \
  --url "$RPM_URL" \
  --maintainer "$RPM_MAINTAINER" \
  --architecture "$RPM_ARCH" \
  --license "$RPM_LICENCE" \
  --description "$RPM_DESCRIPTION" \
  --vendor "$RPM_VENDOR" \
  --depends "git-core" \
  -p "$PACKAGE_PATH" \
  -v "$RPM_VERSION" \
  "./$BUILD_BINARY_PATH=/usr/bin/buildkite-agent" \
  "templates/apt-package/buildkite-agent.env=/etc/buildkite-agent/buildkite-agent.env" \
  "templates/bootstrap.sh=/etc/buildkite-agent/bootstrap.sh" \
  "templates/hooks-unix/checkout.sample=/etc/buildkite-agent/hooks/checkout.sample" \
  "templates/hooks-unix/command.sample=/etc/buildkite-agent/hooks/command.sample" \
  "templates/hooks-unix/post-checkout.sample=/etc/buildkite-agent/hooks/post-checkout.sample" \
  "templates/hooks-unix/pre-checkout.sample=/etc/buildkite-agent/hooks/pre-checkout.sample" \
  "templates/hooks-unix/post-command.sample=/etc/buildkite-agent/hooks/post-command.sample" \
  "templates/hooks-unix/pre-command.sample=/etc/buildkite-agent/hooks/pre-command.sample"

echo ""
echo -e "Successfully created \033[33m$PACKAGE_PATH\033[0m 👏"
echo ""
echo "    # To install this package"
echo "    $ sudo rpm -i $PACKAGE_PATH"
echo ""
echo "    # To uninstall"
echo "    $ sudo rpm -e $RPM_NAME"
echo ""
