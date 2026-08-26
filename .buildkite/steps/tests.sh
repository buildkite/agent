#!/usr/bin/env bash
set -euo pipefail

go version
echo arch is "$(uname -m)"

RACE=''
if [[ $* == *-race* ]] ; then
  RACE='-race'
fi

goos="$(go env GOOS)"
goarch="$(go env GOARCH)"
export AGENT_GO_VERSION="$(go env GOVERSION | cut -d. -f1,2)"

if [[ "${goos}" == "windows" ]]; then
  cache_home="${USERPROFILE}"
else
  cache_home="${HOME}"
fi

if [[ -n "${RACE}" ]]; then
  gocache_name="gocache_race"
  export GOCACHE="${cache_home}/.gocache_race"
else
  gocache_name="gocache"
  export GOCACHE="${cache_home}/.gocache"
fi
export GOMODCACHE="${cache_home}/.gomodcache"

echo --- :inbox_tray: Restoring Go caches
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

# Shard 0 is the single writer for its platform's build cache, on every
# platform including linux/amd64. Compiled test binaries only exist in a cache
# a test run wrote, so the test jobs cannot share a key with a step that does
# not build them.
if [[ "${BUILDKITE_PARALLEL_JOB:-0}" == "0" ]]; then
  save_names=(--name "${gocache_name}")

  # Lint writes linux/amd64 modules. Race jobs never write this shared cache.
  if [[ -z "${RACE}" && "${goos}/${goarch}" != "linux/amd64" ]]; then
    save_names+=(--name gomodcache)
  fi

  echo --- :outbox_tray: Saving Go caches
  buildkite-agent cache save "${save_names[@]}"
fi
