#!/usr/bin/env bash
set -euo pipefail

go version
echo arch is "$(uname -m)"

# Detect race mode first -- it selects a SEPARATE gocache key + dir, because
# -race compiles to a different build hash than normal objects, so the two
# can't share one cache.
RACE=''
if [[ $* == *-race* ]] ; then
  RACE='-race'
fi

goos="$(go env GOOS)"
goarch="$(go env GOARCH)"

# The cache dir must resolve to the SAME location the agent expands.
if [[ "${goos}" == "windows" ]]; then
  cache_home="${USERPROFILE}"
else
  cache_home="${HOME}"
fi

# Non-race builds use gocache (~/.gocache). Race builds use a dedicated
# gocache_race (~/.gocache_race).
if [[ -n "${RACE}" ]]; then
  gocache_name="gocache_race"
  export GOCACHE="${cache_home}/.gocache_race"
else
  gocache_name="gocache"
  export GOCACHE="${cache_home}/.gocache"
fi
export GOMODCACHE="${cache_home}/.gomodcache"

echo --- :inbox_tray: cache restore
buildkite-agent cache restore --name gomodcache --name "${gocache_name}"

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

# One writer per key, gated to shard 0 so parallel shards don't race it.
if [[ "${BUILDKITE_PARALLEL_JOB:-0}" == "0" ]] && ! [[ "${goos}/${goarch}" == "linux/amd64" && -z "${RACE}" ]]; then
  echo --- :outbox_tray: cache save
  buildkite-agent cache save --name "${gocache_name}"
fi
