#!/bin/bash
set -euo pipefail

AGENT_VERSION=$(buildkite-agent meta-data get "agent-version") \
  buildkite-agent pipeline upload .buildkite/pipeline.release.yml
