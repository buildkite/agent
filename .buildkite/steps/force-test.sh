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
echo "wrote: $(cat force-test-checksum-data/payload.txt)"
echo "-- save #1, no --force (expect: create, new checksum-derived address):"
run cache save --name force_test_checksum

echo "payload-build-${BUILDKITE_BUILD_NUMBER}-v2" > force-test-checksum-data/payload.txt
echo "wrote: $(cat force-test-checksum-data/payload.txt)"
echo "-- save #2, STILL no --force (expect: create again, NOT skipped -- checksum changed the address):"
run cache save --name force_test_checksum

echo "-- clearing local dir and restoring:"
rm -rf force-test-checksum-data
run cache restore --name force_test_checksum
echo "restored: $(cat force-test-checksum-data/payload.txt)"
echo "(should match v2 -- the exact-match address for the latest checksum)"

echo "--- :white_check_mark: force-test.sh done"
