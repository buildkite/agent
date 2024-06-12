#!/usr/bin/env bash
#
# This is the installer for the Buildkite Agent.
#
# For more information, see: https://github.com/buildkite/agent

set -e

COMMAND="bash -c \"\`curl -sL https://raw.githubusercontent.com/buildkite/agent/master/install.sh\`\""

echo -e "\033[33m
  _           _ _     _ _    _ _                                _
 | |         (_) |   | | |  (_) |                              | |
 | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_
 | '_ \| | | | | |/ _\` | |/ / | __/ _ \  / _\` |/ _\` |/ _ \ '_ \| __|
 | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_
 |_.__/ \__,_|_|_|\__,_|_|\_\_|\__\___|  \__,_|\__, |\___|_| |_|\__|
                                                __/ |
                                               |___/\033[0m"

echo -e "Finding latest release..."

SYSTEM=$(uname -s | awk '{print tolower($0)}')
MACHINE=$(uname -m | awk '{print tolower($0)}')

if [[ ($SYSTEM == *"mac os x"*) || ($SYSTEM == *darwin*) ]]; then
  PLATFORM="darwin"
elif [[ ($SYSTEM == *"freebsd"*) ]]; then
  PLATFORM="freebsd"
else
  PLATFORM="linux"
fi

if [ -n "$BUILDKITE_INSTALL_ARCH" ]; then
  ARCH="$BUILDKITE_INSTALL_ARCH"
  echo "Using explicit arch '$ARCH'"
else
  case $MACHINE in
    *amd64*)   ARCH="amd64"   ;;
    *x86_64*)
      ARCH="amd64"

      # On Apple Silicon Macs, the architecture reported by `uname` depends on
      # the architecture of the shell, which is in turn influenced by the
      # *terminal*, as *child processes prefer their parents' architecture*.
      #
      # This means that for Terminal.app with the default shell it will be
      # arm64, but x86_64 for people using (pre-3.4.0 builds of) iTerm2 or
      # x86_64 shells.
      #
      # Based on logic in Homebrew: https://github.com/Homebrew/brew/pull/7995
      if [[ "$PLATFORM" == "darwin" && "$(/usr/sbin/sysctl -n hw.optional.arm64 2> /dev/null)" == "1" ]]; then
        ARCH="arm64"
      fi
      ;;
    *arm64*)
      ARCH="arm64"
      ;;
    *armv8*)   ARCH="arm64"   ;;
    *armv7*)   ARCH="armhf"   ;;
    *armv6l*)  ARCH="arm"     ;;
    *armv6*)   ARCH="armhf"   ;;
    *arm*)     ARCH="arm"     ;;
    *ppc64le*) ARCH="ppc64le" ;;
    *aarch64*) ARCH="arm64"   ;;
    *mips64*) ARCH="mips64le" ;;
    *s390x*)   ARCH="s390x"   ;;
    *)
      ARCH="386"
      echo -e "\n\033[36mWe don't recognise the $MACHINE architecture; falling back to $ARCH\033[0m"
      ;;
  esac
fi

if [[ "$BETA" == "true" ]]; then
  RELEASE_INFO_URL="https://buildkite.com/agent/releases/latest?platform=$PLATFORM&arch=$ARCH&prerelease=true&system=$SYSTEM&machine=$MACHINE"
else
  RELEASE_INFO_URL="https://buildkite.com/agent/releases/latest?platform=$PLATFORM&arch=$ARCH&system=$SYSTEM&machine=$MACHINE"
fi

if command -v wget >/dev/null; then
  LATEST_RELEASE=$(wget -qO- "$RELEASE_INFO_URL")
else
  LATEST_RELEASE=$(curl -s "$RELEASE_INFO_URL")
fi

VERSION=$(echo "$LATEST_RELEASE"      | awk -F= '/version=/  { print $2 }')
DOWNLOAD_FILENAME=$(echo "$LATEST_RELEASE"     | awk -F= '/filename=/ { print $2 }')
DOWNLOAD_URL=$(echo "$LATEST_RELEASE" | awk -F= '/url=/      { print $2 }')

function buildkite-download {
  BUILDKITE_DOWNLOAD_TMP_FILE="/tmp/buildkite-download-$$.txt"

  if command -v wget >/dev/null
  then
    wget "$1" -O "$2" 2> $BUILDKITE_DOWNLOAD_TMP_FILE || BUILDKITE_DOWNLOAD_EXIT_STATUS=$?
  else
    curl -L -o "$2" "$1" 2> $BUILDKITE_DOWNLOAD_TMP_FILE || BUILDKITE_DOWNLOAD_EXIT_STATUS=$?
  fi

  if [[ $BUILDKITE_DOWNLOAD_EXIT_STATUS -ne 0 ]]; then
    echo -e "\033[31mFailed to download file: $1\033[0m\n"

    cat $BUILDKITE_DOWNLOAD_TMP_FILE
    exit "$BUILDKITE_DOWNLOAD_EXIT_STATUS"
  fi
}

echo -e "Installing Version: \033[35mv$VERSION\033[0m"

# Default the destination folder
: "${DESTINATION:="$HOME/.buildkite-agent"}"

# If they have a $HOME/.buildkite folder, rename it to `buildkite-agent` and
# symlink back to the old one. Since we changed the name of the folder, we
# don't want any scripts that the user has written that may reference
# ~/.buildkite to break.
if [[ -d "$HOME/.buildkite" && ! -d "$HOME/.buildkite-agent" ]]; then
  mv "$HOME/.buildkite" "$HOME/.buildkite-agent"
  ln -s "$HOME/.buildkite-agent" "$HOME/.buildkite"

  echo ""
  echo "======================= IMPORTANT UPGRADE NOTICE =========================="
  echo ""
  echo "Hey!"
  echo ""
  echo "Sorry to be a pain, but we've renamed ~/.buildkite to ~/.buildkite-agent"
  echo ""
  echo "I've renamed your .buildkite folder to .buildkite-agent, and created a symlink"
  echo "from the old location to the new location, just in case you had any scripts that"
  echo "referenced the previous location."
  echo ""
  echo "If you have any questions, feel free to email me at: keith@buildkite.com"
  echo ""
  echo "~ Keith"
  echo ""
  echo "=========================================================================="
  echo ""
fi

mkdir -p "$DESTINATION"

if [[ ! -w "$DESTINATION" ]]; then
  echo -e "\n\033[31mUnable to write to destination \`$DESTINATION\`\n\nYou can change the destination by running:\n\nDESTINATION=/my/path $COMMAND\033[0m\n"
  exit 1
fi

echo -e "Destination: \033[35m$DESTINATION\033[0m"

echo -e "Downloading $DOWNLOAD_URL"

# Create a temporary folder to download the binary to
INSTALL_TMP=/tmp/buildkite-agent-install-$$
mkdir -p $INSTALL_TMP

# If the file already exists in a folder called releases. This is useful for
# local testing of this file.
if [[ -e releases/$DOWNLOAD ]]; then
  echo "Using existing release: releases/$DOWNLOAD_FILENAME"
  cp releases/"$DOWNLOAD_FILENAME" $INSTALL_TMP
else
  buildkite-download "$DOWNLOAD_URL" "$INSTALL_TMP/$DOWNLOAD_FILENAME"
fi

# Extract the download to a tmp folder inside the $DESTINATION
# folder
tar -C "$INSTALL_TMP" -zxf "$INSTALL_TMP"/"$DOWNLOAD_FILENAME"

# Move the buildkite binary into a bin folder
mkdir -p "$DESTINATION"/bin
mv $INSTALL_TMP/buildkite-agent "$DESTINATION"/bin
chmod +x "$DESTINATION"/bin/buildkite-agent

# Copy the latest config file as dist
mv "$INSTALL_TMP"/buildkite-agent.cfg "$DESTINATION"/buildkite-agent.dist.cfg

# Copy the config file if it doesn't exist
if [[ -f $DESTINATION/buildkite-agent.cfg ]]; then
  echo -e "\n\033[36mIgnoring existing buildkite-agent.cfg (see buildkite-agent.dist.cfg for the latest version)\033[0m"
else
  echo -e "\n\033[36mA default buildkite-agent.cfg has been created for you in $DESTINATION\033[0m"

  cp "$DESTINATION"/buildkite-agent.dist.cfg "$DESTINATION"/buildkite-agent.cfg

  # Set their token for them
  if [[ -n $TOKEN ]]; then
    # Need "-i ''" for macOS X and FreeBSD
    if [[ $(uname) == 'Darwin' ]] || [[ $(uname) == 'FreeBSD' ]]; then
      sed -i '' "s/token=\"xxx\"/token=\"$TOKEN\"/g" "$DESTINATION"/buildkite-agent.cfg
    else
      sed -i "s/token=\"xxx\"/token=\"$TOKEN\"/g" "$DESTINATION"/buildkite-agent.cfg
    fi
  else
    echo -e "\n\033[36mDon't forget to update the config with your agent token! You can find it token on your \"Agents\" page in Buildkite\033[0m"
  fi
fi

# Copy the hook samples
mkdir -p "$DESTINATION"/hooks
mv $INSTALL_TMP/hooks/*.sample "$DESTINATION"/hooks

if [[ -f "$INSTALL_TMP/bootstrap.sh" ]]; then
  mv "$INSTALL_TMP/bootstrap.sh" "$DESTINATION"
  chmod +x "$DESTINATION/bootstrap.sh"
fi

echo -e "\n\033[32mSuccessfully installed to $DESTINATION\033[0m

You can now start the agent!

  $DESTINATION/bin/buildkite-agent start

For docs, help and support:

  https://buildkite.com/docs/agent/v3

Happy building! <3
"
