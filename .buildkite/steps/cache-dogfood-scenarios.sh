#!/usr/bin/env bash
set -euo pipefail

echo "--- :inbox_tray: cache restore (scenarios)"
restore_start="$(date +%s)"
buildkite-agent cache restore --name scenarios
echo "CACHE_RESTORE_SECONDS=$(($(date +%s) - restore_start))"

mkdir -p .cache-scenarios-test

if [[ -f .cache-scenarios-test/marker.txt ]]; then
  echo "--- :mag: existing marker (proves restore actually pulled prior content)"
  cat .cache-scenarios-test/marker.txt
else
  echo "--- :mag: no existing marker (expected on a miss / first run)"
fi

echo "--- :pencil: writing new marker"
echo "written_at=$(date -u +%Y-%m-%dT%H:%M:%SZ) build=${BUILDKITE_BUILD_NUMBER:-unknown} job=${BUILDKITE_JOB_ID:-unknown} branch=${BUILDKITE_BRANCH:-unknown}" >> .cache-scenarios-test/marker.txt

echo "--- :outbox_tray: cache save (scenarios)"
save_start="$(date +%s)"
buildkite-agent cache save --name scenarios
echo "CACHE_SAVE_SECONDS=$(($(date +%s) - save_start))"
