#!/usr/bin/env bash
set -euo pipefail

artifacts_build="$(buildkite-agent meta-data get "agent-artifacts-build")"

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

if [[ "${REPOSITORY:-}" == "" ]]; then
  echo "Error: Missing \$REPOSITORY (<user>/<repository>; i.e. buildkite/agent-experimental)"
  exit 1
fi

if [[ "${DISTRO_VERSION:-}" == "" ]]; then
  echo "Error: Missing \$DISTRO_VERSION (<os>/<version>; i.e. rpm_any/rpm_any)"
  exit 1
fi

echo "--- Clearing rpm directory"
rm -rvf rpm
mkdir -p rpm

echo "--- Downloading built rpm packages"
buildkite-agent artifact download --build "${artifacts_build}" "rpm/*.rpm" rpm/

echo "--- Installing dependencies"
gem install package_cloud

echo "--- Requesting OIDC token"
PACKAGECLOUD_TOKEN="$(buildkite-agent oidc request-token --audience "https://packagecloud.io/${REPOSITORY}" --lifetime 300)"
export PACKAGECLOUD_TOKEN

echo "--- Pushing to Packagecloud"
dry_run package_cloud push "${REPOSITORY}/${DISTRO_VERSION}" rpm/*.rpm
