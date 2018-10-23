#!/bin/bash
set -euo pipefail

## This script can be run locally like this:
##
## .buildkite/steps/publish-docker-image.sh (alpine|ubuntu) imagename (stable|experimental|unstable) <version> <build>
## .buildkite/steps/publish-docker-image.sh alpine buildkiteci/agent:lox-manual-build stable 3.1.1

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

# Convert 2.3.2 into [ 2.3.2 2.3 2 ] or 3.0-beta.42 in [ 3.0-beta.42 3.0 3 ]
parse_version() {
  local v="$1"
  IFS='.' read -r -a parts <<< "${v%-*}"

  for idx in $(seq 1 ${#parts[*]}) ; do
    sed -e 's/ /./g' <<< "${parts[@]:0:$idx}"
  done

  [[ "${v%-*}" == "$v" ]] || echo "$v"
}

release_image() {
  local tag="$1"
  echo "--- :docker: Tagging ${target_image}:${tag}"
  dry_run docker tag "$source_image" "${target_image}:$tag"
  dry_run docker push "${target_image}:$tag"
}

variant="${1:-}"
source_image="${2:-}"
codename="${3:-}"
version="${4:-}"
build="${5:-dev}"

target_image="buildkite/agent"
variant_suffix=""

if [[ "$variant" != "alpine" ]] ; then
  variant_suffix="-$variant"
fi

echo "Tagging docker images for $variant/$codename (version $version build $build)"

# variants of edge/experimental
if [[ "$codename" == "experimental" ]] ; then
  release_image "edge-build-${build}${variant_suffix}"
  release_image "edge${variant_suffix}"
fi

# variants of stable - e.g 2.3.2
if [[ "$codename" == "stable" ]] ; then
  for tag in $(parse_version "$version") ; do
    release_image "${tag}${variant_suffix}"
  done
  release_image "${variant}"

  # publish latest and stable only from alpine
  if [[ "$variant" == "alpine" ]] ; then
    release_image "latest"
    release_image "stable"
  fi
fi

# variants of beta/unstable - e.g 3.0-beta.16
if [[ "$codename" == "unstable" ]] ; then
  release_image "beta${variant_suffix}"
  if [[ "$version" =~ -(alpha|beta|rc)\.[0-9]+$ ]] ; then
    release_image "${version}${variant_suffix}"
  fi
fi
