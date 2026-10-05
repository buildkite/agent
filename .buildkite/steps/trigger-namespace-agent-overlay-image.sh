#!/usr/bin/env bash
set -euo pipefail

# Uploads a trigger step that builds and publishes the Namespace agent overlay
# images (buildkite/namespace-agent-image) for the agent version just released.
#
# Must run after github-release.sh and publish-docker-images.sh: the overlay
# build downloads the darwin-arm64 archive from the v<version> GitHub release
# and pins docker.io/buildkite/agent:<version>-ubuntu-22.04.
#
# agent-release-stable (Main cluster) can only trigger
# namespace-agent-overlay-image (Namespace Base Images Deployment cluster) once
# a pipeline.trigger_build.pipeline organization rule allows it.

agent_version="$(buildkite-agent meta-data get "agent-version")"
is_prerelease="$(buildkite-agent meta-data get "agent-is-prerelease")"

if [[ "${is_prerelease}" == "1" ]]; then
  echo "Skipping Namespace agent overlay images, ${agent_version} is a prerelease"
  exit 0
fi

# The overlay scripts expect a bare version such as 4.1.0, without the v prefix
# used by the GitHub release tag.
if [[ ! "${agent_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Error: agent-version '${agent_version}' is not a version like 4.1.0"
  exit 1
fi

# Like the dry_run helpers, only publish when DRY_RUN is explicitly false. The
# step is still uploaded (skipped) so it can be inspected in the UI.
skip="false"
message_suffix=""
if [[ "${DRY_RUN:-}" != "false" ]]; then
  skip='"Dry run: no agent release was published"'
  message_suffix=" (dry-run)"
fi

# The agent release is complete by now, so an overlay failure is reported
# (soft-failed) without failing the release build.
buildkite-agent pipeline upload <<YAML
steps:
  - name: ":rocket: Publish Namespace agent overlay images for ${agent_version}${message_suffix}"
    key: "namespace-agent-overlay-image"
    skip: ${skip}
    trigger: "namespace-agent-overlay-image"
    async: false
    soft_fail: true
    build:
      message: "Buildkite agent v${agent_version} stable release"
      branch: "main"
      commit: "HEAD"
      env:
        BUILDKITE_AGENT_VERSION: "${agent_version}"
YAML
