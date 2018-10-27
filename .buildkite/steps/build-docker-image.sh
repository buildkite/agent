#!/bin/bash
set -euo pipefail

## This script can be run locally like this:
##
## .buildkite/steps/build-docker-image.sh (alpine|ubuntu) (image tag) (codename) (version)
## e.g: .buildkite/steps/build-docker-image.sh alpine buildkiteci/agent:lox-manual-build stable 3.1.1
##
## You can then publish that image with
##
## .buildkite/steps/publish-docker-image.sh alpine buildkiteci/agent:lox-manual-build stable 3.1.1

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

build_docker_image() {
  local image_tag="$1"
  local packaging_dir="$2"

  echo "--- Building :docker: $image_tag"
  cp -a packaging/linux/root/usr/share/buildkite-agent/hooks/ "${packaging_dir}/hooks/"
  cp pkg/buildkite-agent-linux-amd64 "${packaging_dir}/buildkite-agent"
  chmod +x "${packaging_dir}/buildkite-agent"
  docker build --tag "$image_tag" "${packaging_dir}"
}

test_docker_image() {
  local image_tag="$1"

  echo "--- :hammer: Testing $image_tag can run"
  docker run --rm "$image_tag" --version

  echo "--- :hammer: Testing $image_tag can access docker socket"
  docker run --rm -v /var/run/docker.sock:/var/run/docker.sock --entrypoint "docker" "$image_tag" version

  echo "--- :hammer: Testing $image_tag has docker-compose"
  docker run --rm --entrypoint "docker-compose" "$image_tag" --version
}

push_docker_image() {
  local image_tag="$1"
  echo "--- Pushing :docker: image to $image_tag"
  dry_run docker push "$image_tag"
}

variant="${1:-}"
image_tag="${2:-}"
codename="${3:-}"
version="${4:-}"
push="${PUSH_IMAGE:-true}"

if [[ ! "$variant" =~ ^(alpine|ubuntu)$ ]] ; then
  echo "Unknown docker variant $variant"
  exit 1
fi

# Disable pushing if run manually
if [[ -n "$image_tag" ]] ; then
  push="false"
fi

rm -rf pkg
mkdir -p pkg

if [[ -z "$version" ]] ; then
  echo '--- Downloading :linux: binaries from artifacts'
  buildkite-agent artifact download "pkg/buildkite-agent-linux-amd64" .
else
  echo "--- Downloading :linux: binaries for version $version"
  curl -Lf -o pkg/buildkite-agent-linux-amd64 \
    https://download.buildkite.com/agent/${codename}/${version}/buildkite-agent-linux-amd64
fi

if [[ -z "$image_tag" ]] ; then
  echo "--- Getting docker image tag for $variant from build meta data"
  image_tag=$(buildkite-agent meta-data get "agent-docker-image-$variant")
  echo "Docker Image Tag for $variant: $image_tag"
fi

case $variant in
alpine)
  build_docker_image "$image_tag" "packaging/docker/alpine-linux"
  ;;
ubuntu)
  build_docker_image "$image_tag" "packaging/docker/ubuntu-linux"
  ;;
*)
  echo "Unknown variant $variant"
  exit 1
  ;;
esac

test_docker_image "$image_tag"

if [[ $push == "true" ]] ; then
  push_docker_image "$image_tag"
fi
