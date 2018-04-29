#!/bin/bash
set -euo pipefail

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

build_alpine_docker_image() {
  local image_tag="$1"
  local packaging_dir="packaging/docker/alpine-linux"

  echo "--- Building :docker: $image_tag"
  cp pkg/buildkite-agent-linux-amd64 "${packaging_dir}/buildkite-agent"
  chmod +x "${packaging_dir}/buildkite-agent"
  docker build --tag "$image_tag" "${packaging_dir}"
}

test_docker_image() {
  local image_tag="$1"

  echo "--- :hammer: Testing $image_tag can run"
  docker run --rm --entrypoint "buildkite-agent" "$image_tag" --version

  echo "--- :hammer: Testing $image_tag can access docker socket"
  docker run --rm -v /var/run/docker.sock:/var/run/docker.sock --entrypoint "docker" "$image_tag" version

  echo "--- :hammer: Testing $image_tag has docker-compose"
  docker run --rm --entrypoint "docker-compose" "$image_tag" --version
}

push_docker_image() {
  local image_tag="$1"
  echo '--- Pushing :docker: image to buildkiteci/agent'
  dry_run docker push "$image_tag"
}

rm -rf pkg
mkdir -p pkg

echo '--- Downloading :linux: binaries'
buildkite-agent artifact download "pkg/buildkite-agent-linux-amd64" .

variant="$1"
if [[ ! $variant =~ ^(alpine)$ ]] ; then
  echo "Unknown docker variant $variant"
  exit 1
fi

echo "--- Getting docker image tag for $variant from build meta data"
image_tag=$(buildkite-agent meta-data get "agent-docker-image-$variant")
echo "Docker Image Tag for $variant: $image_tag"

build_alpine_docker_image "$image_tag"
test_docker_image "$image_tag"
push_docker_image "$image_tag"
