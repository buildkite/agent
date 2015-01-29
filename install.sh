#!/bin/bash
#
# You can install the Buildkite Agent with the following:
#
#   bash -c "`curl -sL https://raw.githubusercontent.com/buildkite/agent/master/install.sh`"
#
# For more information, see: https://github.com/buildkite/agent

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

COMMAND="bash -c \"\`curl -sL https://raw.githubusercontent.com/buildkite/agent/master/install.sh\`\""

if [ "$BETA" = "true" ]
then
  BETA_URL="https://raw.githubusercontent.com/buildkite/agent/master/install-beta.sh"
  BETA_INSTALLER="/tmp/buildkite-install-beta-$$.sh"

  echo -e "Downloading and running the beta installer from:\n$BETA_URL"

  buildkite-download $BETA_URL $BETA_INSTALLER

  chmod +x $BETA_INSTALLER
  . $BETA_INSTALLER

  exit 0
fi

LATEST_VERSION="0.2"

if [ ! -z "$VERSION" ]; then
  echo "Sorry, we don't support specifying installation versions anymore via the \$VERSION variable."
  echo "Please remove it and run this command again to install v$LATEST_VERSION"
  exit 1
fi

VERSION=$LATEST_VERSION

echo -e "\033[33m

  _           _ _     _ _    _ _                                _
 | |         (_) |   | | |  (_) |                              | |
 | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_
 | '_ \| | | | | |/ _\` | |/ / | __/ _ \  / _\` |/ _\` |/ _ \ '_ \| __|
 | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_
 |_.__/ \__,_|_|_|\__,_|_|\_\_|\__\___|  \__,_|\__, |\___|_| |_|\__|
                                                __/ |
                                               |___/\033[0m

\033[1;32m> RENAME NOTICE
>
> We’ve just changed our company name from Buildbox to Buildkite, so don’t be
> confused if you see the word “buildbox” in the instructions below. The next
> version of buildbox-agent will be renamed to buildkite-agent, and we’ll be
> releasing upgrade instructions when it’s released. In the mean time, just use
> the instructions below. You can read more about the rename on the blog.
> https://buildkite.com/blog/introducing-our-new-name\033[0m

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

INSTALLED_VERSION=`$DESTINATION/buildbox-agent --version`

chmod +x $DESTINATION/buildbox-*

# Clean up the download
rm -f $DESTINATION/$DOWNLOAD

# Copy the bootstrap sample and make sure it's writable
if [[ -e $DESTINATION/bootstrap.sh ]]
then
  echo -e "\n\033[34mSkipping bootstrap.sh installation as it already exists\033[0m"
else
  BOOTSTRAP_URL=https://raw.githubusercontent.com/buildkite/agent/master/templates/0.2/bootstrap.sh
  BOOTSTRAP_DESTINATION=$DESTINATION/bootstrap.sh

  echo -e "Downloading $BOOTSTRAP_URL"

  buildkite-download "$BOOTSTRAP_URL" "$BOOTSTRAP_DESTINATION"

  chmod +x $DESTINATION/bootstrap.sh
fi

# Allow custom setting of the version
if [ -z "$TOKEN" ]; then
  TOKEN="token123"
fi

echo -e "\n\033[32mSuccessfully installed to $DESTINATION\033[0m

You can now run the Buildkite agent like so:

  $DESTINATION/buildbox-agent start --access-token $TOKEN

You can find your agent's Access Token on your Account Settings
page under \"Agents\".

To customize how builds are run on your server, you can edit:

  $DESTINATION/bootstrap.sh

This file is run for every build and it's responsible for checking out
the source code and running the build script.

The source code of the agent is available here:

  https://github.com/buildkite/agent

If you have any questions or need a hand getting things setup,
please email us at: hello@buildkite.com

Happy Building!

<3 Buildkite"
