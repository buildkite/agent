#!/usr/bin/env bash
set -euo pipefail

echo "--- :inbox_tray: cache restore (smoke)"
restore_start="$(date +%s)"
buildkite-agent cache restore --name smoke
echo "CACHE_RESTORE_SECONDS=$(($(date +%s) - restore_start))"

mkdir -p .cache-smoke-test

if [[ -f .cache-smoke-test/marker.txt ]]; then
  echo "--- :mag: existing marker (proves restore actually pulled prior content)"
  cat .cache-smoke-test/marker.txt
else
  echo "--- :mag: no existing marker (expected on a miss / first run)"
fi

echo "--- :pencil: writing new marker"
echo "written_at=$(date -u +%Y-%m-%dT%H:%M:%SZ) build=${BUILDKITE_BUILD_NUMBER:-unknown} job=${BUILDKITE_JOB_ID:-unknown}" >> .cache-smoke-test/marker.txt

echo "--- :outbox_tray: cache save (smoke)"
save_start="$(date +%s)"
buildkite-agent cache save --name smoke
echo "CACHE_SAVE_SECONDS=$(($(date +%s) - save_start))"
