#!/bin/bash

set -euo pipefail

VERSION=$(buildkite-agent meta-data get "agent-version")

if ! grep --quiet "$VERSION" CHANGELOG.md; then
  echo "The CHANGELOG.md is missing an entry for version ${VERSION}"
  exit 1
fi
