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
# The service provides `go` on PATH for bktec's gotest runner (which shells
# out to `go list ./...` to discover packages) and forwards
# BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN into the container.
#
# bktec is downloaded fresh each run from the test-engine-client GitHub
# release. The agent repo's go.mod pins test-engine-client v1.6.0 for use as
# a `go tool` on the runtime test path; that v1 shape predates the `plan`
# subcommand and is intentionally left untouched here. A future change should
# unify both call sites on a single bktec version.

set -euo pipefail

BKTEC_VERSION="${BKTEC_VERSION:-2.5.0}"

echo "+++ :test_tube: Installing bktec v${BKTEC_VERSION}"

case "$(uname -m)" in
  x86_64)         arch=amd64 ;;
  aarch64|arm64)  arch=arm64 ;;
  *)
    echo "Unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

bindir="$(mktemp -d)"
url="https://github.com/buildkite/test-engine-client/releases/download/v${BKTEC_VERSION}/bktec_${BKTEC_VERSION}_linux_${arch}"

curl --fail --silent --show-error --location --output "${bindir}/bktec" "${url}"
chmod +x "${bindir}/bktec"
export PATH="${bindir}:${PATH}"

bktec --version

echo "+++ :test_tube: Collecting git commit metadata via bktec plan (discarded)"

# bktec needs a writable RESULT_PATH for plan output config validation. The
# --json output is redirected to /dev/null below so this file is never read.
export BUILDKITE_TEST_ENGINE_RESULT_PATH=/tmp/bktec-plan-metadata.json

BKTEC_PREVIEW_SELECTION=1 bktec plan --json --collect-git-metadata > /dev/null

echo "Plan request issued -- git commit metadata sent to Test Engine."
