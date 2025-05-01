#!/usr/bin/env bash
set -euo pipefail

go version
echo arch is "$(uname -m)"

go install gotest.tools/gotestsum@v1.8.0

go install github.com/buildkite/test-engine-client@v1.5.0-rc.3

echo '+++ Running tests'
export BUILDKITE_TEST_ENGINE_SUITE_SLUG=buildkite-agent
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=gotest
export BUILDKITE_TEST_ENGINE_RESULT_PATH="junit-${BUILDKITE_JOB_ID}.xml"
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=1
export BUILDKITE_TEST_ENGINE_TEST_CMD="gotestsum --junitfile={{resultPath}} -- -count=1 -coverprofile=cover.out $@ {{packages}}"

test-engine-client

echo 'Producing coverage report'
go tool cover -html cover.out -o cover.html
