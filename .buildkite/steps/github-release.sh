#!/usr/bin/env bash
set -e

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

echo '--- Getting credentials from SSM'
export GITHUB_RELEASE_ACCESS_TOKEN=$(aws ssm get-parameter --name /pipelines/agent/GITHUB_RELEASE_ACCESS_TOKEN --with-decryption --output text --query Parameter.Value --region us-east-1)

if [[ "$GITHUB_RELEASE_ACCESS_TOKEN" == "" ]]; then
  echo "Error: Missing \$GITHUB_RELEASE_ACCESS_TOKEN"
  exit 1
fi

echo '--- Getting agent version from build meta data'

export FULL_AGENT_VERSION=$(buildkite-agent meta-data get "agent-version-full")
export AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")
export BUILD_VERSION=$(buildkite-agent meta-data get "agent-version-build")
export IS_PRERELEASE=$(buildkite-agent meta-data get "agent-is-prerelease")

echo "Full agent version: $FULL_AGENT_VERSION"
echo "Agent version: $AGENT_VERSION"
echo "Build version: $BUILD_VERSION"
echo "Is prerelease?: $IS_PRERELEASE"

if [[ "$CODENAME" == "unstable" && "$IS_PRERELEASE" == "0" ]] ; then
  echo "Skipping github release, will happen in stable pipeline"
  exit 0
fi

if [[ "$CODENAME" == "stable" && "$IS_PRERELEASE" == "1" ]] ; then
  echo "Skipping github release, should have happened in unstable pipeline"
  exit 0
fi

echo '--- Downloading releases'

artifacts_build=$(buildkite-agent meta-data get "agent-artifacts-build")

rm -rf releases
mkdir -p releases
buildkite-agent artifact download --build "$artifacts_build" "releases/*" .

echo "Version is $FULL_AGENT_VERSION"

export GITHUB_RELEASE_REPOSITORY="buildkite/agent"

if [[ "$IS_PRERELEASE" == "1" ]]; then
  echo "--- 🚀 $AGENT_VERSION (prerelease)"

  buildkite-agent meta-data set github_release_type "prerelease"
  buildkite-agent meta-data set github_release_version "$AGENT_VERSION"

  dry_run github-release "v$AGENT_VERSION" releases/* --commit "$(git rev-parse HEAD)" --prerelease
else
  echo "--- 🚀 $AGENT_VERSION"

  buildkite-agent meta-data set github_release_type "stable"
  buildkite-agent meta-data set github_release_version "$AGENT_VERSION"

  dry_run github-release "v$AGENT_VERSION" releases/* --commit "$(git rev-parse HEAD)"
fi
