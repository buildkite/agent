#!/usr/bin/env bash
set -euo pipefail

go version
echo arch is "$(uname -m)"

# $HOME-anchored, not cwd-relative -- matches cache.yml's ~-anchored
# target_paths (see cache.yml for why a relative "../" path doesn't work:
# the archiver chroots relative paths to cwd and rejects escaping it).
export GOCACHE="$HOME/.gocache"
export GOMODCACHE="$HOME/.gomodcache"

echo --- :inbox_tray: cache restore
buildkite-agent cache restore --name gomodcache --name gocache

RACE=''
if [[ $* == *-race* ]] ; then
  RACE='-race'
fi

export BUILDKITE_TEST_ENGINE_SUITE_SLUG=buildkite-agent
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=gotest
export BUILDKITE_TEST_ENGINE_RESULT_PATH="junit-${BUILDKITE_JOB_ID}.xml"
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=0
if [[ "$(go env GOOS)" == "windows" ]]; then
  # I can't get windows to work with the $COVERAGE_DIR, I tried cygpath but no luck.
  # need a Windows VM to debug.
  export BUILDKITE_TEST_ENGINE_TEST_CMD="go tool gotestsum --junitfile={{resultPath}} -- -count=1 $* {{packages}}"
else
  COVERAGE_DIR="${PWD}/coverage-$(go env GOOS)-$(go env GOARCH)${RACE}"
  mkdir -p "${COVERAGE_DIR}"
  export BUILDKITE_TEST_ENGINE_TEST_CMD="go tool gotestsum --junitfile={{resultPath}} -- -count=1 -cover $* {{packages}} -test.gocoverdir=${COVERAGE_DIR}"
fi

go tool test-engine-client run
