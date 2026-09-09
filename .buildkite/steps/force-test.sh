#!/usr/bin/env bash
set -euo pipefail

echo "--- :hammer: Building test-agent binary from this branch"
go build -o ./bin/test-agent .
run() { ./bin/test-agent "$@"; }

echo "--- :package: Scenario A: same record, force-saved again (static key)"
mkdir -p force-test-static-data
echo "content-build-${BUILDKITE_BUILD_NUMBER}-attempt-1" > force-test-static-data/marker.txt
echo "wrote: $(cat force-test-static-data/marker.txt)"
echo "-- save #1 (create-or-skip, depends on prior runs):"
run cache save --name force_test_static

echo "content-build-${BUILDKITE_BUILD_NUMBER}-attempt-2" > force-test-static-data/marker.txt
echo "wrote: $(cat force-test-static-data/marker.txt)"
echo "-- save #2, no --force (expect: already exists, skipped):"
run cache save --name force_test_static

echo "-- save #3, WITH --force (expect: overwritten):"
run cache save --name force_test_static --force

echo "-- clearing local dir and restoring:"
rm -rf force-test-static-data
run cache restore --name force_test_static
echo "restored: $(cat force-test-static-data/marker.txt)"
echo "(should match attempt-2 content above, proving the force-save overwrote the same address)"

echo "--- :package: Scenario B: a change (checksum-based key)"
mkdir -p force-test-checksum-data
echo "payload-build-${BUILDKITE_BUILD_NUMBER}-v1" > force-test-checksum-data/payload.txt
echo "marker-v1" > force-test-checksum-data/marker.txt
echo "wrote: $(cat force-test-checksum-data/payload.txt)"
echo "-- save #1, no --force (expect: create, new checksum-derived address):"
run cache save --name force_test_checksum

echo "payload-build-${BUILDKITE_BUILD_NUMBER}-v2" > force-test-checksum-data/payload.txt
echo "marker-v2" > force-test-checksum-data/marker.txt
echo "wrote: $(cat force-test-checksum-data/payload.txt)"
echo "-- save #2, STILL no --force (expect: create again, NOT skipped -- checksum changed the address):"
run cache save --name force_test_checksum

# NOTE: don't `rm -rf` the target dir before restoring here. payload.txt is
# the checksum source for cache_key -- restore has to resolve the key (by
# re-hashing payload.txt) *before* it knows what to fetch, so deleting it
# first breaks key resolution outright (that's what happened last run).
# Instead, leave payload.txt as its v2 content and only dirty the
# non-keyed file, to prove restore actually replaces the whole target
# directory rather than just leaving what's already there.
echo "STALE-SHOULD-BE-OVERWRITTEN-BY-RESTORE" > force-test-checksum-data/marker.txt
echo "-- restoring (payload.txt kept at v2 so its checksum resolves; marker.txt deliberately stale):"
run cache restore --name force_test_checksum
echo "restored payload: $(cat force-test-checksum-data/payload.txt)"
echo "restored marker:  $(cat force-test-checksum-data/marker.txt)"
echo "(payload should read v2 -- the exact-match address for the latest checksum; marker should read marker-v2, proving restore replaced the whole directory instead of leaving the stale sentinel)"

echo "--- :white_check_mark: force-test.sh done"
