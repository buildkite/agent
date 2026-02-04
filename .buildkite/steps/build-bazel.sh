#!/usr/bin/env bash

set -Eeufo pipefail

# When it comes time to run Bazel within a pipeline, this script will be ready to do so.

# Run the full build first...
bazelisk build //...

# Artefact the built binary...
buildkite-agent artifact upload "./bazel-bin/buildkite-agent_/buildkite-agent"
