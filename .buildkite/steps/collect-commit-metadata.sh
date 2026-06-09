#!/usr/bin/env bash
# Collect git commit metadata via `bktec plan --collect-git-metadata`.
#
# This is a metadata-only side-channel for the buildkite-agent Test Engine
# suite. The plan output is discarded -- the existing test steps continue to
# use their declared `parallelism:` values unchanged. The first purpose of
# this step is to ship commit / branch / diff metadata to the Test Engine
# plan metadata pipeline on every build.
#
# See TE-5828 (and the bk/bk-rspec reference implementation in TE-5766) for
# context. The step is soft-failed at the pipeline level so a Test Engine API
# hiccup never blocks the build.
#
# Secondary purpose (TE-6071): on feature-branch builds, this step also
# requests an xgboost-ordered test selection plan (--selection-strategy
# xgboost --selection-param score_cutoff=0, mirroring the bk/bk-rspec
# rspec_collect_metadata step in TE-5867). The selection request drives a
# test-prediction inference for the buildkite-agent suite through the shared
# `test-prediction-mme` Multi-Model Endpoint, against that suite's per-suite
# TargetModel. Combined with the already-live bk/bk-rspec selection path,
# this validates the MME serving multiple models for multiple suites (the
# TE-5970 cutover goal). The plan output is still discarded -- the gotest
# steps keep their declared `parallelism:`.
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

# Feature-branch builds (TE-6071): also request an xgboost-ranked plan so the
# request exercises the buildkite-agent TargetModel through the MME.
#
# Skipped on `main`: xgboost requires a non-empty `files_changed`. On `main`,
# $BUILDKITE_PULL_REQUEST_BASE_BRANCH is unset and bktec falls back to
# `origin/main`, producing an empty diff (HEAD == origin/main), which the
# Test Engine API rejects. The metadata-only request still runs on `main`.
# See the bk/bk-rspec rspec_collect_metadata / rspec_plan steps for the same
# guard and the `--metadata base_branch=HEAD^` main workaround we deliberately
# do not duplicate here (this validation is about feature-branch selection).
#
# `--selection-param score_cutoff=0` forces the server-side selector to admit
# (rank) every candidate rather than capping at the default count_cutoff.
# Mirrors the bk/bk-rspec xgboost path. Files are ranked, not filtered.
selection_flags=()
if [ "${BUILDKITE_BRANCH:-}" != "main" ]; then
  selection_flags=(
    --selection-strategy "xgboost"
    --selection-param "score_cutoff=0"
  )
fi

# `bktec plan --json` emits a JSON object to stdout of the shape:
#   {"BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER":"<id>",
#    "BUILDKITE_TEST_ENGINE_PARALLELISM":"<int-as-string>"}
# Capture it so the plan identifier can be stashed as build metadata;
# parallelism is ignored -- the gotest steps keep their declared values.
plan_output=$(BKTEC_PREVIEW_SELECTION=1 go tool test-engine-client plan \
  --json \
  --collect-git-metadata \
  "${selection_flags[@]}")

echo "Plan request issued -- git commit metadata sent to Test Engine."

# TE-6071: when we requested an xgboost-ranked plan, stash the plan
# identifier as build metadata so a follow-up can correlate the build with
# the discarded plan it produced (mirrors the bk/bk-rspec
# rspec_xgboost_plan_id metadata). Parse with ruby (present in this image;
# jq is not) and skip silently if the field is absent -- this is a
# measurement aid, not a build-correctness signal.
if [ ${#selection_flags[@]} -gt 0 ]; then
  plan_id=$(printf '%s' "$plan_output" \
    | ruby -rjson -e 'puts (JSON.parse(STDIN.read)["BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER"] rescue "")')
  if [ -n "$plan_id" ]; then
    buildkite-agent meta-data set "agent_xgboost_plan_id" "$plan_id"
    echo "Stashed xgboost plan id as build metadata: $plan_id"
  else
    echo "No plan identifier returned; skipping agent_xgboost_plan_id metadata."
  fi
fi
