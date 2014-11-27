#!/bin/bash
set -e
set -x

BASE_DIRECTORY=`pwd`

PKG_DIRECTORY=$BASE_DIRECTORY/pkg
TEMPLATE_DIRECTORY=$BASE_DIRECTORY/templates

# Some validation
if [ ! -d "$TEMPLATE_DIRECTORY" ]; then
  echo "Missing templates directory. This script should be run from inside the agent folder like so:"
  echo "cd agent && ./scripts/build.sh"
  exit 1
fi

function build {
  # The name of the binary
  BINARY_FILENAME=buildbox-agent

  # The base name of the agent
  FOLDER_NAME="$BINARY_FILENAME-$1-$2"

  # The name of the folder we'll build the binary in
  BUILD_DIRECTORY="$PKG_DIRECTORY/$FOLDER_NAME"

  # Add .exe for Windows builds
  if [ "$1" == "windows" ]; then
    BINARY_FILENAME="$BINARY_FILENAME.exe"
  fi

  # Build the binary
  GOOS=$1 GOARCH=$2 go build -o $BUILD_DIRECTORY/$BINARY_FILENAME *.go

  # Move into the built directory
  cd $PKG_DIRECTORY/$FOLDER_NAME

  # We need to use .zip for windows builds
  if [ "$1" == "windows" ]; then
    # Add in the additional Windows files
    cp $TEMPLATE_DIRECTORY/bootstrap.bat .
    cp $TEMPLATE_DIRECTORY/start.bat .

    # Zip up the contents of the directory
    zip -X -r "../$FOLDER_NAME.zip" *
  else
    # Use tar on non-windows platforms
    tar cfvz ../$FOLDER_NAME.tar.gz $BINARY_FILENAME
  fi

  # Now back to the PKG_DIRECTORY
  cd ../../

  # Remove the built folder
  rm -rf pkg/$FOLDER_NAME
}

# Prepare the package folder
if [ -d "$PKG_DIRECTORY" ]; then
  rm -rf "$PKG_DIRECTORY"
fi
mkdir -p "$PKG_DIRECTORY"

build "windows" "386"
build "windows" "amd64"
build "linux" "amd64"
build "linux" "386"
build "linux" "arm"
build "darwin" "386"
build "darwin" "amd64"
