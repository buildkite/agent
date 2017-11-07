#!/bin/bash

set -euo pipefail

# Checks that we've updated the changelog. If there's no changelog entry for the
# current version, this puts a big warning on the top of the build so that we
# can remember to update it before cutting a release

VERSION=$(buildkite-agent meta-data get "agent-version")

if ! grep --quiet "$VERSION" CHANGELOG.md; then
  buildkite-agent annotate --style error --context missing-changelog-entry "The CHANGELOG.md is missing an entry for version ${VERSION}"
fi
