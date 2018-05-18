#!/bin/bash
set -euo pipefail

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing \$CODENAME (stable, experimental or unstable)"
  exit 1
fi

version=$(buildkite-agent meta-data get "agent-version")
build=$(buildkite-agent meta-data get "agent-version-build")

for variant in "alpine" "ubuntu" ; do
  echo "--- Getting docker image tag for $variant from build meta data"
  source_image=$(buildkite-agent meta-data get "agent-docker-image-$variant")
  echo "Docker Image Tag for $variant: $source_image"

  echo "--- :docker: Pulling prebuilt image"
  dry_run docker pull "$source_image"

  echo "--- :docker: Publishing images for $variant"
  .buildkite/steps/publish-docker-image.sh "$variant" "$source_image" "$CODENAME" "$version" "$build"
done

