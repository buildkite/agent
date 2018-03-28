#!/bin/bash
set -euo pipefail

: "${DOCKER_IMAGE:=buildkite/agent}"
: "${PREBUILT_IMAGE:=}"
: "${CODENAME:=stable}"
: "${STABLE_VERSION:=2}"

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

is_stable_version() {
  local v="$1"
  for stable_tag in $(parse_version "$STABLE_VERSION") ; do
    if [[ "$v" == "$stable_tag" ]] ; then
      return 0
    fi
  done
  return 1
}

release_image() {
  local tag="$1"
  echo "--- :docker: Tagging ${DOCKER_IMAGE}:${tag}"
  dry_run docker tag "$PREBUILT_IMAGE" "${DOCKER_IMAGE}:$tag"
  dry_run docker push "${DOCKER_IMAGE}:$tag"
}

if [[ -z "${PREBUILT_IMAGE}" ]] ; then
  echo '--- Getting prebuilt docker image from build meta data'
  AGENT_VERSION=$(buildkite-agent meta-data get "agent-docker-image-alpine")
  echo "Docker image: $PREBUILT_IMAGE"
fi

# echo "Parsing $AGENT_VERSION into $(parse_version "$AGENT_VERSION")"

if [[ -z "${AGENT_VERSION:-}" ]] ; then
  echo '--- Getting agent version from build meta data'
  AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")
  echo "Agent version: $AGENT_VERSION"
fi

if [[ "$DOCKER_PULL" =~ (true|1) ]] ; then
  echo '--- :docker: Pulling prebuilt image'
  dry_run docker pull "$PREBUILT_IMAGE"
fi

# variants of edge/experimental
if [[ "$CODENAME" == "experimental" ]] ; then
  release_image "edge-build-${BUILDKITE_BUILD_NUMBER}"
  release_image "edge"
fi

# variants of stable - e.g 2.3.2
if [[ "$CODENAME" == "stable" ]] ; then
  for tag in latest stable $(parse_version "$AGENT_VERSION") ; do
    release_image "$tag"
  done
fi

# variants of beta/unstable - e.g 3.0-beta.16
if [[ "$CODENAME" == "unstable" ]] ; then
  for tag in beta $(parse_version "$AGENT_VERSION") ; do
    if is_stable_version "$tag" ; then
      echo "--- :docker: Skipping tagging stable ${DOCKER_IMAGE}:${tag}"
      continue
    fi
    release_image "$tag"
  done
fi
