#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${BUILDKITE_TRIGGERED_FROM_BUILD_ID:-}" ]] ; then
	echo "Running e2e tests on the agent that's currently running"
	# For now, e2e test the agent that's currently running
	CI_E2E_TESTS_AGENT_PATH="$(which buildkite-agent)"
	export CI_E2E_TESTS_AGENT_PATH
else
	echo "Running e2e tests on the agent from the triggering build"
	# Download the artifact from the triggering build
	ARTIFACT="pkg/buildkite-agent-$(go env GOOS)-$(go env GOARCH)"
	buildkite-agent artifact download "${ARTIFACT}" . --build "${BUILDKITE_TRIGGERED_FROM_BUILD_ID}"
	chmod +x "${ARTIFACT}"
	export CI_E2E_TESTS_AGENT_PATH="${PWD}/${ARTIFACT}"
fi

go tool gotestsum --junitfile junit.xml -- -tags e2e ./internal/e2e/...
