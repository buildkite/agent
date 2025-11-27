#!/usr/bin/env bash
set -euo pipefail

# TODO: download the agent to test as an artifact
#buildkite-agent artifact download buildkite-agent . --build "${BUILDKITE_TRIGGERED_FROM_BUILD_ID}"
#export CI_E2E_TESTS_AGENT_PATH="${PWD}/buildkite-agent"

# For now, e2e test the agent that's currently running
export CI_E2E_TESTS_AGENT_PATH="$(which buildkite-agent)"
go test -v -tags e2e ./internal/e2e/... | sed -e 's/^/>  /'
