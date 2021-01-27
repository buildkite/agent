#!/bin/bash
set -euo pipefail

# Generates and uploads pipeline steps for the edge, beta and stable release

trigger_step() {
  local name="$1"
  local trigger_pipeline="$2"
  local branch="master"
  local message_suffix=""

  if [[ "${DRY_RUN:-false}" == "true" ]] ; then
    branch="$BUILDKITE_BRANCH"
    message_suffix=" (dry-run)"
  fi

  cat <<YAML
  - name: ":rocket: ${name}${message_suffix}"
    trigger: "${trigger_pipeline}"
    async: false
    branches: "${branch}"
    build:
      message: "Release for ${agent_version}, build ${build_version}${message_suffix}"
      commit: "${BUILDKITE_COMMIT}"
      branch: "${BUILDKITE_BRANCH}"
      meta_data:
        agent-artifacts-build: "${BUILDKITE_BUILD_ID}"
        agent-version: "${agent_version}"
        agent-version-build: "${build_version}"
        agent-version-full:  "${full_agent_version}"
        agent-docker-image-alpine: "${agent_docker_image_alpine}"
        agent-is-prerelease: "${agent_is_prerelease}"
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

if [[ "${BUILDKITE_BRANCH}" != "master" && "${DRY_RUN}" != "true" ]] ; then
  echo "No release steps to be uploaded"
  exit 0
fi

agent_version=$(buildkite-agent meta-data get "agent-version")
build_version=$(buildkite-agent meta-data get "agent-version-build")
full_agent_version=$(buildkite-agent meta-data get "agent-version-full")
agent_docker_image_alpine=$(buildkite-agent meta-data get "agent-docker-image-alpine")
agent_is_prerelease=$(buildkite-agent meta-data get "agent-is-prerelease")

# If there is already a release (which means a tag), we want to avoid trying to create
# another one, as this will fail and cause partial broken releases
if git ls-remote --tags origin | grep "refs/tags/v${agent_version}" ; then
  echo "Tag refs/tags/v${agent_version} already exists"
  exit 0
fi

output_steps_yaml | buildkite-agent pipeline upload
