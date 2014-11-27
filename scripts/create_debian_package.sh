#!/bin/bash

VERSION="1.0.0"
NAME="buildbox-agent"
MAINTAINER="<dev@buildbox.io>"
URL="https://buildbox.io/agent"

function package {
  if [ "$1" == "amd64" ]; then
    ARCH="x86_64"
  elif [ "$1" == "386" ]; then
    ARCH="i386"
  else
    echo "Unknown architecture: $1"
    exit 1
  fi

  BUILD_TARGET_PATH="pkg/buildbox-agent-linux-$1"

  PACKAGE_NAME=$NAME"_"$VERSION"_"$1".deb"
  PACKAGE_PATH="pkg/$PACKAGE_NAME"

  echo "-- Building debian package $PACKAGE_NAME"

  # Remove the existing package if it exists
  if [[ -e $PACKAGE_PATH ]]; then
    rm -rf $PACKAGE_PATH
  fi

  # Unzip the tar to the right folder
  mkdir -p $BUILD_TARGET_PATH
  tar -xvf "$BUILD_TARGET_PATH.tar.gz" -C $BUILD_TARGET_PATH

  FPM_BUILD=$(fpm -s dir \
                -t deb \
                -n $NAME \
                --url $URL \
                --maintainer $MAINTAINER \
                --architecture $ARCH \
                --config-files /etc/buildbox-agent/buildbox-agent.conf \
                --config-files /etc/buildbox-agent/bootstrap.sh \
                --deb-upstart templates/deb/buildbox-agent.upstart \
                -p $PACKAGE_PATH \
                -v $VERSION \
                "./$BUILD_TARGET_PATH/buildbox-agent"=/usr/bin/buildbox-agent \
                templates/deb/buildbox-agent.conf=/etc/buildbox-agent/buildbox-agent.conf \
                templates/bootstrap.sh=/etc/buildbox-agent/bootstrap.sh)

  # Capture the exit status for fpm build
  FPM_EXIT_STATUS=$?

  # Remove the folder we created
  rm -rf "$BUILD_TARGET_PATH"

  # Did the fpm build fail?
  if [[ $FPM_EXIT_STATUS -ne "0" ]]; then
    echo "ERROR: Failed to create $PACKAGE_PATH"
    echo -e $FPM_BUILD
    exit $FPM_EXIT_STATUS
  fi

  echo ""
  echo -e "Successfully created \033[33m$PACKAGE_PATH\033[0m 👍"
  echo ""
  echo "You can test installation with:"
  echo ""
  echo "    sudo dpkg -i $PACKAGE_PATH"
  echo ""
  echo "And uninstall it afterwards with:"
  echo ""
  echo "    sudo dpkg --purge $NAME"
  echo ""
}

package "amd64"
package "386"
