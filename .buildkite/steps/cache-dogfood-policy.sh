#!/usr/bin/env bash
# Deliberately no `set -e`: with an empty-rules (default-deny) policy on the
# target registry, both restore and save are expected to fail with 403. We
# want to see both outcomes in one run, not abort after the first failure.
set -uo pipefail

echo "--- :inbox_tray: cache restore (policy_test) -- targeting registry via BUILDKITE_AGENT_CACHE_REGISTRY=${BUILDKITE_AGENT_CACHE_REGISTRY:-unset}"
buildkite-agent cache restore --name policy_test
echo "RESTORE_EXIT=$?"

mkdir -p .cache-policy-test
echo "written_at=$(date -u +%Y-%m-%dT%H:%M:%SZ) build=${BUILDKITE_BUILD_NUMBER:-unknown} job=${BUILDKITE_JOB_ID:-unknown}" >> .cache-policy-test/marker.txt

echo "--- :outbox_tray: cache save (policy_test)"
buildkite-agent cache save --name policy_test
echo "SAVE_EXIT=$?"
