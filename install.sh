#!/bin/bash
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

SYSTEM=$(uname -s | awk '{print tolower($0)}')
MACHINE=$(uname -m | awk '{print tolower($0)}')

if [[ ($SYSTEM == *"mac os x"*) || ($SYSTEM == *darwin*) ]]; then
  PLATFORM="darwin"
elif [[ ($SYSTEM == *"freebsd"*) ]]; then
  PLATFORM="freebsd"
else
  PLATFORM="linux"
fi

# Set up a flag for whether we were passed a destination folder
if [[ -z "$DESTINATION" ]]; then
  DESTINATION_UNSPECIFIED=true
fi

# Default the destination folder
: "${DESTINATION:="$HOME/.buildkite-agent"}"

# Centralised check for whether a beta was requested
function buildkite-requested-beta {
  [[ "$BETA" == "true" ]]
}

# Centralised check for whether Homebrew should be used
# 
# - We want to allow beta installation, which we don't deliver via Brew
# - We only supply Homebrew binaries on macOS
# - If the user supplied a specific destination, we want to honour it
# - Allow opting out by setting the BUILDKITE_HOMEBREW variable to "false"
# - And of course, finally, check if Homebrew is even installed
function buildkite-use-homebrew {
  ! buildkite-requested-beta && \
  [[ \
    "$PLATFORM" == "darwin" && \
    "$DESTINATION_UNSPECIFIED" == "true" && \
    "$BUILDKITE_HOMEBREW" != "false" \
  ]] && \
  command -v brew >/dev/null
}

if buildkite-use-homebrew; then
  echo -e "Installing latest release using Homebrew..."
  echo -e "\033[36mNote: To install without using Homebrew, either set BUILDKITE_HOMEBREW to false or specify a DESTINATION\033[0m\n"
else
  echo -e "Finding latest release..."
fi

# On Apple Silicon Macs, the architecture reported by `uname` depends on the
# architecture of the shell, which is in turn influenced by the *terminal*,
# as *child processes prefer their parents' architecture*.
# 
# This means that for Terminal.app with the default shell it will be arm64,
# but x86_64 for people using (pre-3.4.0 builds of) iTerm2 or x86_64 shells.
#
# Based on logic in Homebrew at https://github.com/Homebrew/brew/pull/7995
function buildkite-cpu-arm64 {
  [[ "$PLATFORM" == "darwin" && "$(/usr/sbin/sysctl -n hw.optional.arm64 2> /dev/null)" == "1" ]]
}

# If we are running on macOS and on Apple Silicon, we force the ARCH to amd64
# to take advantage of Rosetta 2, and emit a special message for those users.
function buildkite-apple-silicon-check {
  if buildkite-cpu-arm64; then
    ARCH="amd64"
    echo -e "\n\033[35mHi there, adventurer! \033[36mWe don't yet have a binary for macOS on Apple Silicon; relying on Rosetta 2 and using $ARCH instead!\033[0m\n"
  fi
}

if [ -n "$BUILDKITE_INSTALL_ARCH" ]; then
  ARCH="$BUILDKITE_INSTALL_ARCH"
  echo "Using explicit arch '$ARCH'"
else
  case $MACHINE in
    *amd64*)   ARCH="amd64"   ;;
    *x86_64*)
      ARCH="amd64"
      # x86_64 is reported by Apple Silicon Macs
      # when the shell is running inside Rosetta 2
      buildkite-apple-silicon-check
      ;;
    *arm64*)
      ARCH="arm64"
      # ARM64 is the native arch on Apple Silicon Macs,
      # but we only have amd64 builds to offer, so this
      # check can override for compatibility
      buildkite-apple-silicon-check
      ;;
    *armv8*)   ARCH="arm64"   ;;
    *armv7*)   ARCH="armhf"   ;;
    *armv6l*)  ARCH="arm"     ;;
    *armv6*)   ARCH="armhf"   ;;
    *arm*)     ARCH="arm"     ;;
    *ppc64le*) ARCH="ppc64le" ;;
    *)
      ARCH="386"
      echo -e "\n\033[36mWe don't recognise the $MACHINE architecture; falling back to $ARCH\033[0m"
      ;;
  esac
fi

# If they have a $HOME/.buildkite folder, rename it to `buildkite-agent` and
# symlink back to the old one. Since we changed the name of the folder, we
# don't want any scripts that the user has written that may reference
# ~/.buildkite to break.
function buildkite-upgrade-dot-directory {
  if [[ -d "$HOME/.buildkite" && ! -L "$HOME/.buildkite" && ! -d "$HOME/.buildkite-agent" ]]; then
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
}

function buildkite-set-token {
  # Set their token for them
  if [[ -n $TOKEN ]]; then
    # Need "-i ''" for macOS and FreeBSD
    if [[ $PLATFORM == 'darwin' || $PLATFORM == 'freebsd' ]]; then
      sed -i '' "s/token=\"xxx\"/token=\"$TOKEN\"/g" "$1"
    else
      sed -i "s/token=\"xxx\"/token=\"$TOKEN\"/g" "$1"
    fi
  else
    echo -e "\n\033[36mDon't forget to update the config with your agent token! You can find it token on your \"Agents\" page in Buildkite\033[0m"
  fi
}

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
    exit $BUILDKITE_DOWNLOAD_EXIT_STATUS
  fi
}

# Function for doing a manual install using the Buildkite release URL and GitHub Releases
function buildkite-manual-install {
  if buildkite-requested-beta; then
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

  echo -e "Installing Version: \033[35mv$VERSION\033[0m"

  buildkite-upgrade-dot-directory

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

    buildkite-set-token "$DESTINATION"/buildkite-agent.cfg
  fi

  # Copy the hook samples
  mkdir -p "$DESTINATION"/hooks
  mv $INSTALL_TMP/hooks/*.sample "$DESTINATION"/hooks

  if [[ -f "$INSTALL_TMP/bootstrap.sh" ]]; then
    mv "$INSTALL_TMP/bootstrap.sh" "$DESTINATION"
    chmod +x "$DESTINATION/bootstrap.sh"
  fi
}

# Function for doing an automated install using Homebrew on macOS
function buildkite-homebrew-install {
  HOMEBREW_FORMULA_NAME="buildkite/buildkite/buildkite-agent"

  # Use `brew list` to check whether any versions are installed, and
  # thus decide whether we need to upgrade or install the new formula.
  # 
  # Note that we do not do an explicit `brew update` ourselves;
  # most people will have Homebrew set to do this automatically,
  # and if we're doing a fresh install it doesn't matter.
  if brew list --versions "$HOMEBREW_FORMULA_NAME" >/dev/null 2>&1; then
    brew upgrade "$HOMEBREW_FORMULA_NAME"
  else
    brew install "$HOMEBREW_FORMULA_NAME"
  fi

  buildkite-set-token "$(brew --prefix)"/etc/buildkite-agent/buildkite-agent.cfg

  buildkite-upgrade-dot-directory

  DESTINATION="$(brew --prefix "$HOMEBREW_FORMULA_NAME")"
}

BUILDKITE_AGENT_BINARY="buildkite-agent"

if buildkite-use-homebrew; then
  buildkite-homebrew-install
else
  buildkite-manual-install
  BUILDKITE_AGENT_BINARY="$DESTINATION/bin/$BUILDKITE_AGENT_BINARY"
fi

echo -e "\n\033[32mSuccessfully installed to $DESTINATION\033[0m

You can now start the agent!

  $BUILDKITE_AGENT_BINARY start

For docs, help and support:

  https://buildkite.com/docs/agent/v3

Happy building! <3
"
