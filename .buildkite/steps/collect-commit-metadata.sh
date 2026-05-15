#!/usr/bin/env bash
# Collect git commit metadata via `bktec plan --collect-git-metadata`.
#
# This is a metadata-only side-channel for the buildkite-agent Test Engine
# suite. The plan output is discarded -- the existing test steps continue to
# use their declared `parallelism:` values unchanged. The only purpose of this
# step is to ship commit / branch / diff metadata to the Test Engine plan
# metadata pipeline on every build.
#
# See TE-5828 (and the bk/bk-rspec reference implementation in TE-5766) for
# context. The step is soft-failed at the pipeline level so a Test Engine API
# hiccup never blocks the build.
#
# Runs inside the `agent` docker-compose service (see
# .buildkite/docker-compose.yml), which is a linux/amd64+arm64 golang image.
# The service provides `go` on PATH so we can install bktec via `go tool`,
# and forwards BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN into the container.
#
# bktec is pinned via go.mod and installed as a `go tool`, sharing the install
# path with `.buildkite/steps/tests.sh`. See TE-5842 for the unification.

set -euo pipefail

echo "+++ :test_tube: bktec version"
go tool test-engine-client --version

echo "+++ :test_tube: Collecting git commit metadata via bktec plan (discarded)"

# bktec needs a writable RESULT_PATH for plan output config validation. The
# --json output is redirected to /dev/null below so this file is never read.
export BUILDKITE_TEST_ENGINE_RESULT_PATH=/tmp/bktec-plan-metadata.json

BKTEC_PREVIEW_SELECTION=1 go tool test-engine-client plan --json --collect-git-metadata > /dev/null

echo "Plan request issued -- git commit metadata sent to Test Engine."
