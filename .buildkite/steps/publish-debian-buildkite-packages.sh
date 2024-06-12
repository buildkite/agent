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

echo "--- Clearing deb directory"
rm -rvf deb
mkdir -p deb

echo "--- Downloading built debian packages"
buildkite-agent artifact download --build "${artifacts_build}" "deb/*.deb" deb/

echo "--- Requesting OIDC token"
export TOKEN="$(buildkite-agent oidc request-token --audience "https://packages.buildkite.com/${REGISITRY}" --lifetime 300)"

echo "--- Pushing to Packagecloud"
ORGANIZATION_SLUG="${REGISTRY%*/}"
REGISTRY_SLUG="${REGISTRY#/*}"
for FILE in deb/*.deb; do
  dry_run curl -X POST "https://api.buildkite.com/v2/packages/organizations/${ORGANIZATION_SLUG}/registries/${REGISTRY_SLUG}/packages" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file=@$FILE"
done
