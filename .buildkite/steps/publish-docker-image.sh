#!/usr/bin/env bash
set -euo pipefail

## This script can be run locally like this:
##
## .buildkite/steps/publish-docker-image.sh (docker.io|ghcr.io|packages.buildkite.com) (alpine|ubuntu) imagename (stable|experimental|unstable) <version> <build>
## e.g.
## .buildkite/steps/publish-docker-image.sh docker.io alpine buildkiteci/agent:lox-manual-build stable 3.1.1

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

skopeo() {
  docker run --rm \
    -v "${DOCKER_CONFIG:-$HOME/.docker}:/root/.docker:ro" \
    -e "REGISTRY_AUTH_FILE=/root/.docker/config.json" \
    quay.io/skopeo/stable:v1 \
    "$@"
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

  case "${registry}" in
  docker.io)
    echo "--- :docker: Copying ${target_image}:${tag} to Docker Hub"
    dry_run skopeo copy --multi-arch all "docker://${source_image}" "docker://docker.io/buildkite/${target_image}:${tag}"
    ;;
  ghcr.io)
    echo "--- :github: Copying ${target_image}:${tag} to GHCR"
    dry_run skopeo copy --multi-arch all "docker://${source_image}" "docker://ghcr.io/buildkite/${target_image}:${tag}"
    ;;
  packages.buildkite.com)
    # OIDC tokens only last 5 minutes, and issuing them is cheap, so log in as close as possible to the push
    buildkite-agent oidc request-token \
      --audience "https://packages.buildkite.com/buildkite/agent-docker" \
      --lifetime 300 \
      | docker login packages.buildkite.com/buildkite/agent-docker --username=buildkite --password-stdin

    echo "--- :buildkite: Copying ${target_image}:${tag} to Buildkite Packages"
    dry_run skopeo copy --multi-arch all "docker://${source_image}" "docker://packages.buildkite.com/buildkite/agent-docker/${target_image}:${tag}"
    ;;
  *)
    echo "+++ Registry '${registry}' is not supported\!"
    exit 1
    ;;
  esac
}

registry="${1:-}"
variant="${2:-}"
source_image="${3:-}"
codename="${4:-}"
version="${5:-}"
build="${6:-dev}"

target_image="agent"
variant_suffix=""

if [[ "${variant}" != "alpine" ]] ; then
  variant_suffix="-${variant}"
fi

echo "Tagging docker images for $variant/$codename (version $version build $build)"

# variants of edge/experimental
if [[ "${codename}" == "experimental" ]] ; then
  release_image "edge-build-${build}${variant_suffix}"
  release_image "edge${variant_suffix}"
fi

# variants of stable - e.g 2.3.2
if [[ "${codename}" == "stable" ]] ; then
  for tag in $(parse_version "${version}") ; do
    release_image "${tag}${variant_suffix}"
  done
  release_image "${variant}"

  # publish bare 'ubuntu' only from ubuntu-22.04
  if [[ "${variant}" == "ubuntu-22.04" ]] ; then
    for tag in $(parse_version "${version}") ; do
      release_image "${tag}-ubuntu"
    done
    release_image "ubuntu"
  fi

  # publish latest and stable only from alpine
  if [[ "${variant}" == "alpine" ]] ; then
    release_image "latest"
    release_image "stable"
  fi
fi

# variants of beta/unstable - e.g 3.0-beta.16
if [[ "${codename}" == "unstable" ]] ; then
  release_image "beta${variant_suffix}"
  if [[ "${version}" =~ -(alpha|beta|rc)\.[0-9]+$ ]] ; then
    release_image "${version}${variant_suffix}"
  fi
fi
