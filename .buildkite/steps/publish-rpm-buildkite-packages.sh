#!/bin/bash
set -euo pipefail

artifacts_build="$(buildkite-agent meta-data get "agent-artifacts-build")"

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

if [[ "${REGISTRY:-}" == "" ]]; then
  echo "Error: Missing \$REGISTRY (<organization>/<registry>; i.e. buildkite/agent-experimental)"
  exit 1
fi

echo "--- Clearing rpm directory"
rm -rvf rpm
mkdir -p rpm

echo "--- Downloading built rpm packages"
buildkite-agent artifact download --build "${artifacts_build}" "rpm/*.rpm" rpm/

echo "--- Requesting OIDC token"
export TOKEN="$(buildkite-agent oidc request-token --audience "https://packages.buildkite.com/${REGISTRY}" --lifetime 300)"

echo "--- Pushing to Buildkite Packages"
ORGANIZATION_SLUG="${REGISTRY%*/}"
REGISTRY_SLUG="${REGISTRY#/*}"
for FILE in rpm/*.rpm; do
  dry_run curl -X POST "https://api.buildkite.com/v2/packages/organizations/${ORGANIZATION_SLUG}/registries/${REGISTRY_SLUG}/packages" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file=@$FILE"
done
