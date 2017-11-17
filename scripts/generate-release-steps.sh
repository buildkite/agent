#!/bin/bash
set -euo pipefail

agent_version=$(buildkite-agent meta-data get "agent-version")
build_version=$(buildkite-agent meta-data get "agent-version-build")
full_agent_version=$(buildkite-agent meta-data get "agent-version-full")

trigger_step() {
  local name="$1"
  local trigger_pipeline="$2"

  cat <<YAML
  - name: ":rocket: ${name}"
    trigger: "${trigger_pipeline}"
    async: false
    branches: "master show-version-in-block-steps"
    build:
      message: "Release for ${agent_version}, build ${build_version}"
      commit: "${BUILDKITE_COMMIT}"
      branch: "${BUILDKITE_BRANCH}"
      meta_data:
        agent-artifacts-build: "${BUILDKITE_BUILD_ID}"
        agent-version: "${agent_version}"
        agent-version-build: "${build_version}"
        agent-version-full:  "${full_agent_version}"
      env:
        DRY_RUN: "${DRY_RUN:-false}"
YAML
}

block_step() {
  local label="$1"
  cat <<YAML
  - wait
  - block: "${label}"
YAML
}

echo "steps:"

git fetch --tags

## only allow a release if a release tag for it doesn't exist
if ! git rev-parse -q --verify "refs/tags/v${agent_version}" >/dev/null; then
  exit 0
fi

trigger_step \
  "edge ${agent_version}.${build_version}" \
  "agent-release-experimental"

block_step "beta?"

trigger_step \
  "beta ${agent_version}" \
  "agent-release-unstable"

if [[ ! $agent_version =~ (beta|rc) ]] ; then
  block_step "stable?"

  trigger_step \
    "stable ${agent_version}" \
    "agent-release-stable"
fi
