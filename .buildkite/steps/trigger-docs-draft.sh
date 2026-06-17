#!/usr/bin/env bash

set -euo pipefail

# Extract the PR number from the commit message.
# Handles both formats:
#   Squash merge: "Some title (#1234)"
#   Merge commit: "Merge pull request #1234 from ..."
PR_NUMBER="$(echo "${BUILDKITE_MESSAGE}" | grep -oP '#\K[0-9]+' | head -1 || true)"

if [ -z "${PR_NUMBER}" ]; then
  echo "Could not extract PR number from commit message, skipping"
  exit 0
fi

echo "--- :memo: Triggering docs-draft-writer for buildkite/agent#${PR_NUMBER}"

buildkite-agent pipeline upload <<EOF
steps:
  - trigger: "docs-draft-writer"
    label: ":memo: Docs draft for buildkite/agent#${PR_NUMBER}"
    async: true
    soft_fail: true
    build:
      message: "Docs draft for buildkite/agent#${PR_NUMBER}"
      branch: "main"
      env:
        UPSTREAM_REPO: "buildkite/agent"
        UPSTREAM_PR_NUMBER: "${PR_NUMBER}"
EOF
