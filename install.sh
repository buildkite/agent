#!/usr/bin/env bash
#
# This is the installer for the Buildkite Agent.
#
# For more information, see: https://github.com/buildkite/agent

set -eu

COMMAND="bash -c \"\`curl -sL https://raw.githubusercontent.com/buildkite/agent/main/install.sh\`\""

echo -e "\033[33m
  _           _ _     _ _    _ _                                _
 | |         (_) |   | | |  (_) |                              | |
 | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_
 | '_ \| | | | | |/ _\` | |/ / | __/ _ \  / _\` |/ _\` |/ _ \ '_ \| __|
 | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_
 |_.__/ \__,_|_|_|\__,_|_|\_\_|\__\___|  \__,_|\__, |\___|_| |_|\__|
                                                __/ |
                                               |___/\033[0m"

SYSTEM="$(uname -s | awk '{print tolower($0)}')"
MACHINE="$(uname -m | awk '{print tolower($0)}')"

if [[ ("${SYSTEM}" == *"mac os x"*) || ("${SYSTEM}" == *darwin*) ]]; then
  PLATFORM="darwin"
elif [[ ("${SYSTEM}" == *"freebsd"*) ]]; then
  PLATFORM="freebsd"
else
  PLATFORM="linux"
fi

if [ -n "${BUILDKITE_INSTALL_ARCH:-}" ]; then
  ARCH="${BUILDKITE_INSTALL_ARCH}"
  echo "Using explicit arch '${ARCH}'"
else
  case "${MACHINE}" in
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
      if [[ "${PLATFORM}" == "darwin" && "$(/usr/sbin/sysctl -n hw.optional.arm64 2> /dev/null)" == "1" ]]; then
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
    *riscv64*) ARCH="riscv64" ;;
    *)
      ARCH="386"
      echo -e "\n\033[36mWe don't recognise the ${MACHINE} architecture; falling back to ${ARCH}\033[0m"
      ;;
  esac
fi

if command -v curl >/dev/null 2>&1 ; then
  HTTP_GET="curl -LsS"
elif command -v wget >/dev/null 2>&1 ; then
  HTTP_GET="wget -qO-"
else
  echo -e "\033[31mCouldn't find either curl or wget on the system\!\033[0m\n"
  echo -e "\033[31mMake sure either curl or wget is installed and findable within \$PATH\!\033[0m\n"
  exit 1
fi

if [[ "${BUILDKITE_AGENT_VERSION:-latest}" == "latest" ]]; then
    echo -e "Finding latest release..."

    RELEASE_INFO_URL="https://buildkite.com/agent/releases/latest?platform=${PLATFORM}&arch=${ARCH}&system=${SYSTEM}&machine=${MACHINE}"
    if [[ "${BETA:-}" == "true" ]]; then
      RELEASE_INFO_URL="${RELEASE_INFO_URL}&prerelease=true"
    fi

    LATEST_RELEASE="$(eval "${HTTP_GET} '${RELEASE_INFO_URL}'")"

    VERSION="$(          echo "${LATEST_RELEASE}" | awk -F= '/version=/  { print $2 }')"
    DOWNLOAD_FILENAME="$(echo "${LATEST_RELEASE}" | awk -F= '/filename=/ { print $2 }')"
    DOWNLOAD_URL="$(     echo "${LATEST_RELEASE}" | awk -F= '/url=/      { print $2 }')"
else
    VERSION="${BUILDKITE_AGENT_VERSION}"
    DOWNLOAD_FILENAME="buildkite-agent-${PLATFORM}-${ARCH}-${VERSION}.tar.gz"
    DOWNLOAD_URL="https://github.com/buildkite/agent/releases/download/v${VERSION}/${DOWNLOAD_FILENAME}"
fi

if [[ "${DISABLE_CHECKSUM_VERIFICATION:-}" != "true" ]]; then
  if command -v openssl >/dev/null 2>&1 ; then
    SHA256SUM="openssl dgst -sha256 -r"
  elif command -v sha256sum >/dev/null 2>&1 ; then
    SHA256SUM="sha256sum"
  else
    echo -e "\033[31mCouldn't find either openssl or sha256sum on the system\!\033[0m\n"
    echo -e "\033[31mMake sure either openssl or sha256sum is installed and findable within \$PATH\!\033[0m\n"
    echo -e "\033[31mOr, set DISABLE_CHECKSUM_VERIFICATION=true to disable verification.\033[0m\n"
    exit 1
  fi

  SHASUMS_FILE="buildkite-agent-${VERSION}.SHA256SUMS"
  SHASUMS_URL="${DOWNLOAD_URL%"${DOWNLOAD_FILENAME}"}${SHASUMS_FILE}"
  WANT_SHASUM="$(eval "${HTTP_GET} '${SHASUMS_URL}' | awk '/${DOWNLOAD_FILENAME}/ { print \$1 }'")"

  if [[ "${WANT_SHASUM}" == "" ]]; then
    echo -e "\033[31mA SHA256 checksum for ${DOWNLOAD_FILENAME} could not be fetched\!\033[0m\n"
    echo -e "\033[31mPlease retry, and reach out to support@buildkite.com if this error persists.\033[0m\n"
    echo -e "\033[31mIf you want to skip checksum verification, set DISABLE_CHECKSUM_VERIFICATION=true when re-running.\033[0m\n"
    exit 1
  fi
fi

echo -e "Installing Version: \033[35mv${VERSION}\033[0m"

# Default the destination folder
: "${DESTINATION:="${HOME}/.buildkite-agent"}"

# If they have a $HOME/.buildkite folder, rename it to `buildkite-agent` and
# symlink back to the old one. Since we changed the name of the folder, we
# don't want any scripts that the user has written that may reference
# ~/.buildkite to break.
if [[ -d "${HOME}/.buildkite" && ! -d "${HOME}/.buildkite-agent" ]]; then
  mv "${HOME}/.buildkite" "${HOME}/.buildkite-agent"
  ln -s "${HOME}/.buildkite-agent" "${HOME}/.buildkite"

  cat <<EON
======================= IMPORTANT UPGRADE NOTICE ==========================

Hey!

Sorry to be a pain, but we've renamed ~/.buildkite to ~/.buildkite-agent

I've renamed your .buildkite folder to .buildkite-agent, and created a symlink
from the old location to the new location, just in case you had any scripts that
referenced the previous location.

If you have any questions, feel free to email me at: keith@buildkite.com

~ Keith

==========================================================================
EON
fi

# Set up destination paths
mkdir -p "${DESTINATION}"

if [[ ! -w "${DESTINATION}" ]]; then
  echo -e "\n\033[31mUnable to write to destination \`${DESTINATION}\`\n\nYou can change the destination by running:\n\nDESTINATION=/my/path ${COMMAND}\033[0m\n"
  exit 1
fi

mkdir -p "${DESTINATION}/bin"   # for the binary
mkdir -p "${DESTINATION}/hooks" # for hooks
INSTALL_TMP="$(mktemp -d "${DESTINATION}/tmp.XXXXXX")" # for tarball extraction
trap 'rm -fr ${INSTALL_TMP}' EXIT

echo -e "Destination: \033[35m${DESTINATION}\033[0m"

echo -e "Downloading ${DOWNLOAD_URL}"

# If the file already exists in a folder called releases. This is useful for
# local testing of this file.
if [[ -e "releases/${DOWNLOAD_FILENAME}" ]]; then
  echo "Using existing release: releases/${DOWNLOAD_FILENAME}"
  cp "releases/${DOWNLOAD_FILENAME}" "${INSTALL_TMP}/${DOWNLOAD_FILENAME}"
else
  if ! eval "${HTTP_GET} '${DOWNLOAD_URL}'" > "${INSTALL_TMP}/${DOWNLOAD_FILENAME}" ; then
    echo -e "\033[31mFailed to download file: ${DOWNLOAD_FILENAME}\033[0m\n"
    exit 1
  fi
fi

if [[ "${DISABLE_CHECKSUM_VERIFICATION:-}" != "true" ]]; then
  if ! eval "${SHA256SUM} ${INSTALL_TMP}/${DOWNLOAD_FILENAME} | grep -q '${WANT_SHASUM}'" ; then
    echo -e "\033[31m${DOWNLOAD_FILENAME} downloaded, but was corrupted (has the wrong checksum)\033[0m\n"
    echo -e "\033[31mYou might be able to resolve this by retrying.\033[0m\n"
    echo -e "\033[31mTo skip checksum verification, set DISABLE_CHECKSUM_VERIFICATION=true when re-running.\033[0m\n"
    exit 1
  fi
fi

# Extract the download to a tmp folder inside the $DESTINATION
# folder
tar -C "${INSTALL_TMP}" -zxf "${INSTALL_TMP}/${DOWNLOAD_FILENAME}"

# Move the buildkite binary into a bin folder
mv "${INSTALL_TMP}/buildkite-agent" "${DESTINATION}/bin"
chmod +x "${DESTINATION}/bin/buildkite-agent"

# Copy the latest config file as dist
mv "${INSTALL_TMP}/buildkite-agent.cfg" "${DESTINATION}/buildkite-agent.dist.cfg"

# Copy the config file if it doesn't exist
if [[ -f "${DESTINATION}/buildkite-agent.cfg" ]]; then
  echo -e "\n\033[36mIgnoring existing buildkite-agent.cfg (see buildkite-agent.dist.cfg for the latest version)\033[0m"
else
  echo -e "\n\033[36mA default buildkite-agent.cfg has been created for you in ${DESTINATION}\033[0m"

  cp "${DESTINATION}/buildkite-agent.dist.cfg" "${DESTINATION}/buildkite-agent.cfg"

  # Set their token for them
  if [[ -n "${TOKEN:-}" ]]; then
    # Need "-i ''" for macOS X and FreeBSD
    if [[ "$(uname)" == 'Darwin' ]] || [[ "$(uname)" == 'FreeBSD' ]]; then
      sed -i '' "s/token=\"xxx\"/token=\"${TOKEN}\"/g" "${DESTINATION}/buildkite-agent.cfg"
    else
      sed -i "s/token=\"xxx\"/token=\"${TOKEN}\"/g" "${DESTINATION}/buildkite-agent.cfg"
    fi
  else
    echo -e "\n\033[36mDon't forget to update the config with your agent token! You can find it token on your \"Agents\" page in Buildkite\033[0m"
  fi
fi

# Copy the hook samples
mv "${INSTALL_TMP}/hooks/"*.sample "${DESTINATION}/hooks"

if [[ -f "${INSTALL_TMP}/bootstrap.sh" ]]; then
  mv "${INSTALL_TMP}/bootstrap.sh" "${DESTINATION}"
  chmod +x "${DESTINATION}/bootstrap.sh"
fi

echo -e "\n\033[32mSuccessfully installed to ${DESTINATION}\033[0m

You can now start the agent!

  ${DESTINATION}/bin/buildkite-agent start

For docs, help and support:

  https://buildkite.com/docs/agent/v3

Happy building! <3
"
