#!/usr/bin/env bash
set -euo pipefail

if [[ "${DRY_RUN:-}" != false ]]; then
  echo 'Skipping Nix publisher: dry run'
  exit 0
fi

if [[ "$(buildkite-agent meta-data get agent-is-prerelease)" == 1 ]]; then
  echo 'Skipping Nix publisher: prerelease'
  exit 0
fi

version=$(buildkite-agent meta-data get agent-version)
if [[ ! "$version" =~ ^4\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo "Expected a stable v4 agent version, got: $version" >&2
  exit 1
fi

buildkite-agent pipeline upload <<YAML
steps:
  - label: ":nix: Publish Buildkite Agent v${version} for Nix"
    key: agent-release-nix
    trigger: agent-release-nix
    async: false
    soft_fail: true
    build:
      message: "Buildkite Agent v${version} stable release"
      branch: main
      commit: HEAD
      env:
        AGENT_RELEASE_TAG: "v${version}"
YAML
