#!/bin/bash
set -euo pipefail

echo '--- Getting agent version from build meta data'

export FULL_AGENT_VERSION=$(buildkite-agent meta-data get "agent-version-full")
export AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")
export BUILD_VERSION=$(buildkite-agent meta-data get "agent-version-build")
export ARTIFACTS_BUILD=$(buildkite-agent meta-data get "agent-artifacts-build")

YUM_PATH=/yum.buildkite.com

echo "Full agent version: $FULL_AGENT_VERSION"
echo "Agent version: $AGENT_VERSION"
echo "Build version: $BUILD_VERSION"

function build() {
  echo "--- Building rpm package $1/$2"

  BINARY_FILENAME="pkg/buildkite-agent-$1-$2"

  # Download the built binary artifact
  buildkite-agent artifact download --build "$ARTIFACTS_BUILD" "$BINARY_FILENAME" .

  # Make sure it's got execute permissions so we can extract the version out of it
  chmod +x "$BINARY_FILENAME"

  # Build the rpm package using the architecture and binary, they are saved to rpm/
  ./scripts/utils/build-linux-package.sh "rpm" "$2" "$BINARY_FILENAME" "$AGENT_VERSION" "$BUILD_VERSION"
}

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

echo '--- Installing dependencies'
bundle

# Make sure we have a local copy of the yum repo
echo "--- Syncing s3://$RPM_S3_BUCKET to $(hostname)"
mkdir -p $YUM_PATH
dry_run aws --region us-east-1 s3 sync "s3://$RPM_S3_BUCKET" "$YUM_PATH"

# Make sure we have a clean rpm folder
rm -rf rpm

# Build the packages into rpm/
dry_run build "linux" "amd64"
dry_run build "linux" "386"
