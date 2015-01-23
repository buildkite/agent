#!/bin/bash
set -e

if [[ ${#} -ne 2 ]]
then
  echo "Usage: ${0} [platform] [arch]" >&2
  exit 1
fi

function info {
  echo -e "\033[35m$1\033[0m"
}

GOOS=${1}
GOARCH=${2}

BASE_DIRECTORY=`pwd`
RELEASE_DIRECTORY=$BASE_DIRECTORY/pkg/releases
TEMPLATE_DIRECTORY=$BASE_DIRECTORY/templates

# Make sure the template directory is there
if [ ! -d "$TEMPLATE_DIRECTORY" ]; then
  echo "Missing templates directory. This script should be run from inside the agent folder like so:"
  echo "cd agent && ./scripts/build.sh"
  exit 1
fi

# The name of the binary
BINARY_FILENAME=buildkite-agent

# The base name of the agent
FOLDER_NAME="$BINARY_FILENAME-$GOOS-$GOARCH"

# The name of the folder we'll build the binary in
BUILD_DIRECTORY="$RELEASE_DIRECTORY/$FOLDER_NAME"

# Add .exe for Windows builds
if [ "$GOOS" == "windows" ]; then
  BINARY_FILENAME="$BINARY_FILENAME.exe"
  RELEASE_FILE_NAME="$FOLDER_NAME.zip"
else
  RELEASE_FILE_NAME="$FOLDER_NAME.tar.gz"
fi

# Remove the release if it already exists
if [ -d "$RELEASE_DIRECTORY/$RELEASE_FILE_NAME" ]; then
  rm -rf "$RELEASE_DIRECTORY/$RELEASE_FILE_NAME"
fi

info "Building the binary"

# Build the binary
go build -v -o $BUILD_DIRECTORY/$BINARY_FILENAME *.go

# Move into the built directory
cd $RELEASE_DIRECTORY/$FOLDER_NAME

# We need to use .zip for windows builds
if [ "$GOOS" == "windows" ]; then
  info "Zipping up the files"

  # Add in the additional Windows files
  cp $TEMPLATE_DIRECTORY/bootstrap.bat .
  cp $TEMPLATE_DIRECTORY/start.bat .

  # Zip up the contents of the directory
  zip -X -r "../$RELEASE_FILE_NAME" *
else
  info "Tarring up the files"

  # Use tar on non-windows platforms
  tar cfvz ../$RELEASE_FILE_NAME $BINARY_FILENAME
fi

# Now back to the RELEASE_DIRECTORY
cd ../../

# Remove the built folder
rm -rf pkg/$FOLDER_NAME

echo -e "👏 Created release \033[33mpkg/releases/$RELEASE_FILE_NAME\033[0m"
