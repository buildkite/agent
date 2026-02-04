#!/usr/bin/env bash

set -euo pipefail

VERSION=$(buildkite-agent meta-data get "agent-version")

if ! grep --quiet "$VERSION" CHANGELOG.md; then
  echo "The CHANGELOG.md is missing an entry for version ${VERSION}"

  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    exit 1
  else
    echo "Dry Run Mode enabled, so allowing the build to continue"
  fi
fi
