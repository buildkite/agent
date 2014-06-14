#!/bin/bash
#
# You can install the Buildbox Agent with the following:
#
#   bash -c "`curl -sL https://raw.githubusercontent.com/buildboxhq/buildbox-agent/master/install.sh`"
#
# For more information, see: https://github.com/buildboxhq/buildbox-agent

COMMAND="bash -c \"\`curl -sL https://raw.githubusercontent.com/buildboxhq/buildbox-agent/master/install.sh\`\""

# Allow custom setting of the version
if [ -z "$VERSION" ]; then
  VERSION="0.1"
fi

set -e

echo -e "\033[33m
  _           _ _     _ _                                        _
 | |         (_) |   | | |                                      | |
 | |__  _   _ _| | __| | |__   _____  __   __ _  __ _  ___ _ __ | |_
 | '_ \| | | | | |/ _\` | '_ \ / _ \ \/ /  / _\` |/ _\` |/ _ \ '_ \| __|
 | |_) | |_| | | | (_| | |_) | (_) >  <  | (_| | (_| |  __/ | | | |_
 |_.__/ \__,_|_|_|\__,_|_.__/ \___/_/\_\  \__,_|\__, |\___|_| |_|\__|
                                                 __/ |
                                                |___/\033[0m
-- https://buildbox.io

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

# Allow custom setting of the destination
if [ -z "$DESTINATION" ]; then
  # But default to the home directory
  DESTINATION="$HOME/.buildbox"
  mkdir -p $DESTINATION
fi

if [ ! -w "$DESTINATION" ]
then
  echo -e "\n\033[31mUnable to write to destination \`$DESTINATION\`\n\nYou can change the destination by running:\n\nDESTINATION=/my/path $COMMAND\033[0m\n"
  exit 1
fi

echo -e "Destination: \033[35m$DESTINATION\033[0m"

# Download and unzip the file to the destination
DOWNLOAD="buildbox-agent-$PLATFORM-$ARCH.tar.gz"
URL="https://github.com/buildboxhq/buildbox-agent/releases/download/v$VERSION/$DOWNLOAD"
echo -e "\nDownloading $URL"

# Remove the download if it already exists
rm -f $DESTINATION/$DOWNLOAD

# If the file already exists in a folder called pkg, just use that. :)
if [[ -e pkg/$DOWNLOAD ]]
then
  cp pkg/$DOWNLOAD $DESTINATION/$DOWNLOAD
else
  # Boo, we don't have it. Download the file then.
  if command -v wget >/dev/null
  then
    wget -q $URL -O $DESTINATION/$DOWNLOAD
  else
    curl -L -s -o $DESTINATION/$DOWNLOAD $URL
  fi
fi

# Extract the download to the destination folder
tar -C $DESTINATION -zxf $DESTINATION/$DOWNLOAD

# Make sure it's exectuable
chmod +x $DESTINATION/buildbox-agent
chmod +x $DESTINATION/buildbox-artifact

# This file isn't availabe in stable yet
if [[ -e $DESTINATION/buildbox-data ]]
then
  chmod +x $DESTINATION/buildbox-data
fi

# Clean up the download
rm -f $DESTINATION/$DOWNLOAD

# Copy the bootstrap sample and make sure it's writable
if [[ -e $DESTINATION/bootstrap.sh ]]
then
  echo -e "\n\033[34mSkipping bootstrap.sh installation as it already exists\033[0m"
else
  BOOTSTRAP_URL=https://raw.githubusercontent.com/buildboxhq/buildbox-agent/master/templates/bootstrap.sh
  BOOTSTRAP_DESTINATION=$DESTINATION/bootstrap.sh

  echo -e "Downloading $BOOTSTRAP_URL"

  if command -v wget >/dev/null
  then
    wget -q $BOOTSTRAP_URL -O $BOOTSTRAP_DESTINATION
  else
    curl -L -s -o $BOOTSTRAP_DESTINATION $BOOTSTRAP_URL
  fi

  chmod +x $DESTINATION/bootstrap.sh
fi

echo -e "\n\033[32mSuccessfully installed to $DESTINATION\033[0m

You can now run the Buildbox agent like so:

  $DESTINATION/buildbox-agent start --access-token token123

You can find your agent's Access Token on your Account Settings
page under \"Agents\".

To customize how builds are run on your server, you can edit:

  $DESTINATION/bootstrap.sh

This file is run for every build and it's responsible for checking out
the source code and running the build script.

The source code of the agent is available here:

  https://github.com/buildboxhq/buildbox-agent

If you have any questions or need a help getting things setup,
please email us at: hello@buildbox.io

Happy Building!

<3 Buildbox"
