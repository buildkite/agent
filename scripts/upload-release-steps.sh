#!/bin/bash
set -euo pipefail

# Generates and uploads pipeline steps for the edge, beta and stable release

trigger_step() {
  local name="$1"
  local trigger_pipeline="$2"

  cat <<YAML
  - name: ":rocket: ${name}"
    trigger: "${trigger_pipeline}"
    async: false
    branches: "master"
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

output_steps_yaml() {
  echo "steps:"

  trigger_step \
    "edge ${agent_version}.${build_version}" \
    "agent-release-edge"

  block_step "beta?"

  trigger_step \
    "beta ${agent_version}" \
    "agent-release-beta"

  if [[ ! "$agent_version" =~ (beta|rc) ]] ; then
    block_step "stable?"

    trigger_step \
      "stable ${agent_version}" \
      "agent-release-stable"
  fi
}

agent_version=$(buildkite-agent meta-data get "agent-version")
build_version=$(buildkite-agent meta-data get "agent-version-build")
full_agent_version=$(buildkite-agent meta-data get "agent-version-full")

git fetch --tags

# If there is already a release (which means a tag), we want to avoid trying to create
# another one, as this will fail and cause partial broken releases
if git rev-parse -q --verify "refs/tags/v${agent_version}" >/dev/null; then
  echo "Tag refs/tags/v${agent_version} already exists"
  exit 0
fi

output_steps_yaml | buildkite-agent pipeline upload
