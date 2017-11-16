#!/bin/bash
set -euo pipefail

export AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")

if [[ $1 == "unstable" ]] ; then
  buildkite-agent pipeline upload .buildkite/pipeline.release-unstable.yml
elif [[ $1 == "stable" && ! $AGENT_VERSION =~ beta|rc ]] ; then
  buildkite-agent pipeline upload .buildkite/pipeline.release-stable.yml
fi
