#!/bin/bash
set -euo pipefail

agent_version=$(awk -F\" '/var baseVersion string = "/ {print $2}' agent/version.go)
build_version=${BUILDKITE_BUILD_NUMBER:-1}
full_agent_version="buildkite-agent version ${agent_version}, build ${build_version}"

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
      commit: "${BUILDKITE_COMMIT:-}"
      branch: "${BUILDKITE_BRANCH:-}"
      meta_data:
        agent-artifacts-build: "${BUILDKITE_BUILD_ID:-}"
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

# TODO:

# is_already_released_beta() {
#   local version="$1"
#   return 0
# }

# is_already_released_stable() {
#   local version="$1"
#   return 0
# }

echo "steps:"

trigger_step \
  "edge ${agent_version}.${build_version}" \
  "agent-release-experimental"

block_step "beta?"

trigger_step \
  "beta ${agent_version}}" \
  "agent-release-unstable"

if [[ ! $agent_version =~ (beta|rc) ]] ; then
  block_step "stable?"

  trigger_step \
    "stable ${agent_version}}" \
    "agent-release-stable"
fi
