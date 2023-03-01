#!/usr/bin/env bash

set -Eeufo pipefail

## This script can be run locally like this:
##
## .buildkite/steps/build-docker-image.sh (alpine|alpine-k8s|ubuntu-18.04|ubuntu-20.04|ubuntu-22.04|sidecar) (image tag) (codename) (version)
## e.g: .buildkite/steps/build-docker-image.sh alpine buildkiteci/agent:lox-manual-build stable 3.1.1
##
## You can then publish that image with
##
## .buildkite/steps/publish-docker-image.sh alpine buildkiteci/agent:lox-manual-build stable 3.1.1
##
## You will need to have the ability to build multiarch docker images.
## This requires packages that are typically named `qemu-user-static` and `qemu-user-static-binfmt`
## to be installed

variant="${1:-}"
image_tag="${2:-}"
codename="${3:-}"
version="${4:-}"
push="${PUSH_IMAGE:-true}"

if [[ ! "$variant" =~ ^(alpine|alpine-k8s|ubuntu-18\.04|ubuntu-20\.04|ubuntu-22\.04|sidecar)$ ]] ; then
  echo "Unknown docker variant $variant"
  exit 1
fi

# Disable pushing if run manually
if [[ -n "$image_tag" ]] ; then
  push="false"
fi

packaging_dir="packaging/docker/$variant"

rm -rf pkg
mkdir -p pkg

echo "--- Building"

for arch in amd64 arm64 ; do
  if [[ -z "$version" ]] ; then
    echo '--- Downloading :linux: binaries from artifacts'
    buildkite-agent artifact download "pkg/buildkite-agent-linux-$arch" .
  else
    echo "--- Downloading :linux: binaries for version $version and architecture $arch"
    curl -Lf -o "pkg/buildkite-agent-linux-$arch" \
      "https://download.buildkite.com/agent/${codename}/${version}/buildkite-agent-linux-$arch"
  fi
  chmod +x "pkg/buildkite-agent-linux-$arch"
done

if [[ -z "$image_tag" ]] ; then
  echo "--- Getting docker image tag for $variant from build meta data"
  image_tag=$(buildkite-agent meta-data get "agent-docker-image-$variant")
  echo "Docker Image Tag for $variant: $image_tag"
fi

# Needed for buildking multiarch
# See https://github.com/docker/buildx/issues/314
QEMU_VERSION=6.2.0
echo "--- Installing QEMU $QEMU_VERSION"
docker run --privileged --userns=host --rm "tonistiigi/binfmt:qemu-v$QEMU_VERSION" --uninstall qemu-*
docker run --privileged --userns=host --rm "tonistiigi/binfmt:qemu-v$QEMU_VERSION" --install all

builder_name=$(docker buildx create --use)
# shellcheck disable=SC2064 # we want the current $builder_name to be trapped, not the runtime one
trap "docker buildx rm $builder_name || true" EXIT

echo "--- Building :docker: $image_tag"
cp -a packaging/linux/root/usr/share/buildkite-agent/hooks/ "${packaging_dir}/hooks/"
cp pkg/buildkite-agent-linux-{amd64,arm64} "$packaging_dir"

# Build images for all architectures
docker buildx build --progress plain --builder "$builder_name" --platform linux/amd64,linux/arm64 "$packaging_dir"
# Tag images for just the native architecture. There is a limitation in docker that prevents this
# from being done in one command. Luckliy the second build will be quick because of docker layer caching
docker buildx build --progress plain --builder "$builder_name" --tag "$image_tag" --load "$packaging_dir"

# Sanity check test before pushing. Only works for current arch. CI will test all arches as well.
.buildkite/steps/test-docker-image.sh "$variant" "$image_tag" "$(uname -m)"

if [[ $push == "true" ]] ; then
  echo "--- Pushing to ECR :ecr:"
  # Do another build with all architectures. The layers should be cached from the previous build
  # with all architectures.
  # Pushing to the docker registry in this way greatly simplifies creating the manifest list on the
  # docker registry so that either architecture can be pulled with the same tag.
  docker buildx build \
    --progress plain \
    --builder "$builder_name" \
    --tag "$image_tag" \
    --platform linux/amd64,linux/arm64 \
    --push \
    "$packaging_dir"
fi
