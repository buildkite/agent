#!/bin/bash
set -euo pipefail

export AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")

buildkite-agent pipeline upload .buildkite/pipeline.release-experimental.yml
sleep 1

buildkite-agent pipeline upload .buildkite/pipeline.release-unstable.yml
sleep 1

if [[ ! AGENT_VERSION =~ beta|rc ]] ; then
  buildkite-agent pipeline upload .buildkite/pipeline.release-stable.yml
fi
