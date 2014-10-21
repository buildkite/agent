#!/bin/bash
#
# You can install the Buildbox Agent with the following:
#
#   bash -c "`curl -sL https://raw.githubusercontent.com/buildbox/agent/master/install.sh`"
#
# For more information, see: https://github.com/buildbox/agent

COMMAND="bash -c \"\`curl -sL https://raw.githubusercontent.com/buildbox/agent/master/install.sh\`\""

if [ "$VERSION" = "1.0-beta.1" ]
then
  echo "NOTICE: Installing 1.0-beta.1 is no longer supported...sorry. Please install 1.0-beta.2"
  exit 1
fi

if [ "$BETA" = "true" ]
then
  VERSION="1.0-beta.4"
fi

# Allow custom setting of the version
if [ -z "$VERSION" ]; then
  VERSION="0.2"
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
URL="https://github.com/buildbox/agent/releases/download/v$VERSION/$DOWNLOAD"
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

INSTALLED_VERSION=`$DESTINATION/buildbox-agent --version`

if [[ "$INSTALLED_VERSION" = "buildbox-agent version 1.0-beta.4" ]]
then
  # Move the buildbox binary into a bin folder
  mkdir -p $DESTINATION/bin
  mv $DESTINATION/buildbox-agent $DESTINATION/bin
  chmod +x $DESTINATION/bin/buildbox-agent

  function shim {
    echo "#!/bin/bash
DIR=\$(cd \"\$( dirname \"\${BASH_SOURCE[0]}\" )\" && pwd)
echo \"###################################################################
DEPRECATED: This binary is deprecated.

Please use: \\\`$DESTINATION/bin/buildbox-agent$1\\\`
###################################################################
\"
exit 1"
  }

  # Deprecate the old 1.0-beta.1 binary.
  if [[ -e $DESTINATION/bin/buildbox ]]
  then
    shim "" > $DESTINATION/bin/buildbox
    chmod +x $DESTINATION/bin/buildbox
  fi

  # Was there a previous agent installed?
  if [[ -e $DESTINATION/buildbox-artifact ]]
  then
    shim " start --token 123" > $DESTINATION/buildbox-agent
    shim " build-artifact" > $DESTINATION/buildbox-artifact
    shim " build-data" > $DESTINATION/buildbox-data
  fi
fi

chmod +x $DESTINATION/buildbox-*

# Clean up the download
rm -f $DESTINATION/$DOWNLOAD

# Copy the bootstrap sample and make sure it's writable
if [[ -e $DESTINATION/bootstrap.sh ]]
then
  echo -e "\n\033[34mSkipping bootstrap.sh installation as it already exists\033[0m"
else
  BOOTSTRAP_URL=https://raw.githubusercontent.com/buildbox/agent/master/templates/bootstrap.sh
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

# Allow custom setting of the version
if [ -z "$TOKEN" ]; then
  TOKEN="token123"
fi

# Switch the start command depending on the version
if [[ -e $DESTINATION/bin/buildbox-agent ]]
then
  START_COMMAND="bin/buildbox-agent start --token $TOKEN"
else
  START_COMMAND="buildbox-agent start --access-token $TOKEN"
fi

echo -e "\n\033[32mSuccessfully installed to $DESTINATION\033[0m

You can now run the Buildbox agent like so:

  $DESTINATION/$START_COMMAND

You can find your agent's Access Token on your Account Settings
page under \"Agents\".

To customize how builds are run on your server, you can edit:

  $DESTINATION/bootstrap.sh

This file is run for every build and it's responsible for checking out
the source code and running the build script.

The source code of the agent is available here:

  https://github.com/buildbox/agent

If you have any questions or need a hand getting things setup,
please email us at: hello@buildbox.io

Happy Building!

<3 Buildbox"
