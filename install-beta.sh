#!/bin/bash
#
# This is the installer for the Buildkite Agent.
#
# For more information, see: https://github.com/buildkite/agent

COMMAND="bash -c \"\`curl -sL https://raw.githubusercontent.com/buildkite/agent/master/install-beta.sh\`\""

VERSION="1.0-beta.7"

set -e

function buildkite-download {
  BUILDKITE_DOWNLOAD_TMP_FILE="/tmp/buildkite-download-$$.txt"

  if command -v wget >/dev/null
  then
    wget $1 -O $2 2> $BUILDKITE_DOWNLOAD_TMP_FILE || BUILDKITE_DOWNLOAD_EXIT_STATUS=$?
  else
    curl -L -o $2 $1 2> $BUILDKITE_DOWNLOAD_TMP_FILE || BUILDKITE_DOWNLOAD_EXIT_STATUS=$?
  fi

  if [[ $BUILDKITE_DOWNLOAD_EXIT_STATUS -ne 0 ]]; then
    echo -e "\033[31mFailed to download file: $1\033[0m\n"

    cat $BUILDKITE_DOWNLOAD_TMP_FILE
    exit $BUILDKITE_DOWNLOAD_EXIT_STATUS
  fi
}

echo -e "\033[33m

  _           _ _     _ _    _ _                                _
 | |         (_) |   | | |  (_) |                              | |
 | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_
 | '_ \| | | | | |/ _\` | |/ / | __/ _ \  / _\` |/ _\` |/ _ \ '_ \| __|
 | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_
 |_.__/ \__,_|_|_|\__,_|_|\_\_|\__\___|  \__,_|\__, |\___|_| |_|\__|
                                                __/ |
                                               |___/\033[0m

Installing Version: \033[35mv$VERSION\033[0m"

UNAME=`uname -sp | awk '{print tolower($0)}'`

if [[ ($UNAME == *"mac os x"*) || ($UNAME == *darwin*) ]]
then
  PLATFORM="darwin"
else
  PLATFORM="linux"
fi

if [[ ($UNAME == *x86_64*) || ($UNAME == *amd64*) ]]
then
  ARCH="amd64"
else
  ARCH="386"
fi

# Default the destination folder and then create it
: ${DESTINATION:="$HOME/.buildkite"}
mkdir -p $DESTINATION

if [ ! -w "$DESTINATION" ]
then
  echo -e "\n\033[31mUnable to write to destination \`$DESTINATION\`\n\nYou can change the destination by running:\n\nDESTINATION=/my/path $COMMAND\033[0m\n"
  exit 1
fi

echo -e "Destination: \033[35m$DESTINATION\033[0m"

# Download and unzip the file to the destination
DOWNLOAD="buildkite-agent-$PLATFORM-$ARCH.tar.gz"
URL="https://github.com/buildkite/agent/releases/download/v$VERSION/$DOWNLOAD"
echo -e "\nDownloading $URL"

# Remove the download if it already exists
rm -f $DESTINATION/$DOWNLOAD

# If the file already exists in a folder called pkg, just use that. :)
if [[ -e pkg/$DOWNLOAD ]]
then
  cp pkg/$DOWNLOAD $DESTINATION/$DOWNLOAD
else
  buildkite-download "$URL" "$DESTINATION/$DOWNLOAD"
fi

# Extract the download to the destination folder
tar -C $DESTINATION -zxf $DESTINATION/$DOWNLOAD

# Move the buildkite binary into a bin folder
mkdir -p $DESTINATION/bin
mv $DESTINATION/buildkite-agent $DESTINATION/bin
chmod +x $DESTINATION/bin/buildkite-agent

# Clean up the download
rm -f $DESTINATION/$DOWNLOAD

# Copy the bootstrap sample and make sure it's executable
if [[ -e $DESTINATION/bootstrap.sh ]]
then
  echo -e "\n\033[34mSkipping bootstrap.sh installation as it already exists\033[0m"
else
  BOOTSTRAP_URL=https://raw.githubusercontent.com/buildkite/agent/master/templates/bootstrap.sh
  BOOTSTRAP_DESTINATION=$DESTINATION/bootstrap.sh

  echo -e "Downloading $BOOTSTRAP_URL"

  buildkite-download "$BOOTSTRAP_URL" "$BOOTSTRAP_DESTINATION"

  chmod +x $DESTINATION/bootstrap.sh
fi

: ${TOKEN:="token123"}

echo -e "\n\033[32mSuccessfully installed to $DESTINATION\033[0m

You can now run the Buildkite Agent like so:

  $DESTINATION/bin/buildkite-agent start --token $TOKEN

You can find your Agent token by going to your organizations \"Agents\" page

The source code of the agent is available here:

  https://github.com/buildkite/agent

If you have any questions or need a hand getting things setup,
please email us at: hello@buildkite.com

Happy Building!

<3 Buildkite"
