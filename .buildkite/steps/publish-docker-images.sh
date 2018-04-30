#!/bin/bash
set -euo pipefail

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

docker_image="buildkite/agent"
stable_version="3"

version=$(buildkite-agent meta-data get "agent-version")
build=$(buildkite-agent meta-data get "agent-version-build")

# Convert 2.3.2 into [ 2.3.2 2.3 2 ] or 3.0-beta.42 in [ 3.0-beta.42 3.0 3 ]
parse_version() {
  local v="$1"
  IFS='.' read -r -a parts <<< "${v%-*}"

  for idx in $(seq 1 ${#parts[*]}) ; do
    sed -e 's/ /./g' <<< "${parts[@]:0:$idx}"
  done

  [[ "${v%-*}" == "$v" ]] || echo "$v"
}

is_stable_version() {
  local v="$1"
  for stable_tag in $(parse_version "$stable_version") ; do
    if [[ "$v" == "$stable_tag" ]] ; then
      return 0
    fi
  done
  return 1
}

release_image() {
  local tag="$1"
  echo "--- :docker: Tagging ${docker_image}:${tag}"
  dry_run docker tag "$image_tag" "${docker_image}:$tag"
  dry_run docker push "${docker_image}:$tag"
}

variant="$1"
suffix=""

if [[ ! "$variant" =~ ^(alpine|ubuntu)$ ]] ; then
  echo "Unknown docker variant $variant"
  exit 1
fi

if [[ "$variant" != "alpine" ]] ; then
  suffix="-$variant"
fi

echo "--- Getting docker image tag for $variant from build meta data"
image_tag=$(buildkite-agent meta-data get "agent-docker-image-$variant")
echo "Docker Image Tag for $variant: $image_tag"

echo "--- :docker: Pulling prebuilt image"
dry_run docker pull "$image_tag"

# variants of edge/experimental
if [[ "$CODENAME" == "experimental" ]] ; then
  release_image "edge-build-${build}${suffix}"
  release_image "edge${suffix}"
fi

# variants of stable - e.g 2.3.2
if [[ "$CODENAME" == "stable" ]] ; then
  for tag in latest stable $(parse_version "$version") ; do
    release_image "$tag${suffix}"
  done
fi

# variants of beta/unstable - e.g 3.0-beta.16
if [[ "$CODENAME" == "unstable" ]] ; then
  for tag in beta $(parse_version "$version") ; do
    if is_stable_version "$tag" ; then
      echo "--- :docker: Skipping tagging stable ${docker_image}:${tag}"
      continue
    fi
    release_image "$tag${suffix}"
  done
fi
