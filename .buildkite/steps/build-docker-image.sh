#!/bin/bash
set -euo pipefail

## This script can be run locally like this:
##
## .buildkite/steps/build-docker-image.sh (alpine|ubuntu|centos) (image tag) (codename) (version)
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
  local platforms="$1"
  local image_tag="$2"
  local packaging_dir="$3"
  local push="$4"

  output_type="docker"
  if [ "$push" = "true" ]
  then
    output_type="registry"
  fi

  echo "--- Building :docker: $image_tag"
  cp -a packaging/linux/root/usr/share/buildkite-agent/hooks/ "${packaging_dir}/hooks/"

  cp pkg/buildkite-agent-* "${packaging_dir}/"
  chmod +x ${packaging_dir}/buildkite-agent-*
  ls -la "${packaging_dir}"

  docker buildx create --use
  docker buildx build --platform "$platforms" --tag "$image_tag" "${packaging_dir}" --output="type=${output_type}"
}

test_docker_image() {
  local image_tag="$1"

  echo "--- :hammer: Testing $image_tag can run the buildkite-agent"
  docker run --rm "$image_tag" --version

  echo "--- :hammer: Testing $image_tag can access docker socket"
  docker run --rm -v /var/run/docker.sock:/var/run/docker.sock --entrypoint "docker" "$image_tag" version

  echo "--- :hammer: Testing $image_tag has docker-compose"
  docker run --rm --entrypoint "docker-compose" "$image_tag" --version
}

push_docker_image() {
  local image_tag="$1"
  echo "--- Pushing :docker: image to $image_tag"
  docker push "$image_tag"
}

variant="${1:-}"
platforms="${2:-linux/amd64}"
image_tag="${3:-}"
codename="${4:-}"
version="${5:-}"
push="${PUSH_IMAGE:-true}"

if [[ ! "$variant" =~ ^(alpine|ubuntu-18\.04|ubuntu-20\.04|centos|sidecar)$ ]] ; then
  echo "Unknown docker variant $variant"
  exit 1
fi

# Disable pushing if run manually
if [[ -n "$image_tag" ]] ; then
  push="false"
fi

rm -rf pkg
mkdir -p pkg

# e.g. linux/amd64,linux/arm64/v8,linux/arm/v7
for platform in ${platforms//,/ }
do
  echo "--- Downloading binaries for ${platform}"

  # What the platform is looking for
  platform_arch="$(echo $platform | cut -d/ -f2)"
  platform_variant="$(echo "$platform" | cut -d/ -f3)"

  # What we call it
  download_query="buildkite-agent-linux-${platform_arch}"
  if [ "$platform_arch" = "arm" && "$platform_variant" = "v7" ]
  then
    download_query="buildkite-agent-linux-armhf"
  fi

  # Download what we call it
  if [[ -z "$version" ]] ; then
    echo "--- Downloading ${platform} binaries from artifacts"
    buildkite-agent artifact download "pkg/${download_query}" .
  else
    echo "--- Downloading ${platform} binaries for version ${version}"
    curl -Lf -o "pkg/${download_query}" \
      "https://download.buildkite.com/agent/${codename}/${version}/${download_query}"
  fi

  # Move it where the platform installer will look for it
  expected="pkg/buildkite-agent-linux-${platform_arch}${platform_variant}"
  if [ "pkg/${download_query}" != "$expected" ]
  then
    mv "pkg/${download_query}" "$expected"
  fi
done

if [[ -z "$image_tag" ]] ; then
  echo "--- Getting docker image tag for $variant from build meta data"
  image_tag=$(buildkite-agent meta-data get "agent-docker-image-$variant")
  echo "Docker Image Tag for $variant: $image_tag"
fi

case $variant in
alpine)
  build_docker_image "$platforms" "$image_tag" "packaging/docker/alpine-linux" "$push"
  ;;
ubuntu-18.04)
  build_docker_image "$platforms" "$image_tag" "packaging/docker/ubuntu-18.04-linux" "$push"
  ;;
ubuntu-20.04)
  build_docker_image "$platforms" "$image_tag" "packaging/docker/ubuntu-20.04-linux" "$push"
  ;;
centos)
  build_docker_image "$platforms" "$image_tag" "packaging/docker/centos-linux" "$push"
  ;;
sidecar)
  build_docker_image "$platforms" "$image_tag" "packaging/docker/sidecar" "$push"
  ;;
*)
  echo "Unknown variant $variant"
  exit 1
  ;;
esac

case $variant in
sidecar)
  echo "Skipping tests for sidecar variant"
  ;;
*)
  test_docker_image "$image_tag"
  ;;
esac
