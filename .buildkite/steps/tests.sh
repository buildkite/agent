#!/usr/bin/env bash
set -euo pipefail

go version
echo arch is "$(uname -m)"

go install gotest.tools/gotestsum@v1.8.0

go install github.com/buildkite/test-engine-client@v1.5.0-rc.3

export BUILDKITE_TEST_ENGINE_SUITE_SLUG=buildkite-agent
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=gotest
export BUILDKITE_TEST_ENGINE_RESULT_PATH="junit-${BUILDKITE_JOB_ID}.xml"
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=1
if [[ "$(go env GOOS)" == "windows" ]]; then
  # I can't get windows to work with the $COVERAGE_DIR, I tried cygpath but no luck.
  # need a Windows VM to debug.
  export BUILDKITE_TEST_ENGINE_TEST_CMD="gotestsum --junitfile={{resultPath}} -- -count=1 $* {{packages}}"
else
  mkdir -p coverage
  COVERAGE_DIR="$PWD/coverage"
  export BUILDKITE_TEST_ENGINE_TEST_CMD="gotestsum --junitfile={{resultPath}} -- -count=1 -cover $* {{packages}} -test.gocoverdir=${COVERAGE_DIR}"
fi

test-engine-client
