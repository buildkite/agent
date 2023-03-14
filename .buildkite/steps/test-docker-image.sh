#!/usr/bin/env bash

set -Eeufo pipefail

variant="${1:-}"
image_tag="${2:-}"
platform="${3:-}"

platform_any_to_uname() {
  case $1 in
    amd64 | x86_64)
      printf "x86_64"
      ;;
    arm64 | aarch64)
      printf "aarch64"
      ;;
  esac
}

expected_platform_uname=$(platform_any_to_uname "$platform")
if [[ -z $expected_platform_uname ]] ; then
  expected_platform_uname=$(uname -m)
fi

if [[ -z "$image_tag" ]] ; then
  echo "--- Getting docker image tag for $variant from build meta data"
  image_tag=$(buildkite-agent meta-data get "agent-docker-image-$variant")
  echo "Docker Image Tag for $variant: $image_tag"
fi

test_platform() {
  echo "--- :hammer: Testing $image_tag platform"
  actual_platform=$(docker run --rm --platform "$platform" --entrypoint uname "$image_tag" -m)
  if [[ $actual_platform != "$expected_platform_uname" ]] ; then
    echo "Error: expected $expected_platform_uname, received $actual_platform"
    exit 1
  fi
}

test_buildkite_agent() {
  echo "--- :hammer: Testing $image_tag can run"
  docker run --rm --platform "$platform" "$image_tag" --version
}

test_buildkite_agent_sidecar() {
  echo "--- :hammer: Testing $image_tag can run"
  docker run --rm --platform "$platform" "$image_tag" /buildkite/bin/buildkite-agent --version
}

test_docker_socket() {
  echo "--- :hammer: Testing $image_tag can access docker socket"
  docker run --rm --platform "$platform" -v /var/run/docker.sock:/var/run/docker.sock --entrypoint docker "$image_tag" version
}

test_docker_compose() {
  echo "--- :hammer: Testing $image_tag has docker-compose"
  docker run --rm --platform "$platform" --entrypoint docker-compose "$image_tag" --version
}

test_docker_compose_v2() {
  echo "--- :hammer: Testing $image_tag has docker compose v2"
  docker run --rm --platform "$platform" --entrypoint docker "$image_tag" compose version
}

test_docker_buildx() {
  echo "--- :hammer: Testing $image_tag has buildx"
  docker run --rm --platform "$platform" --entrypoint docker "$image_tag" buildx version
}

test_tini() {
  echo "--- :hammer: Testing $image_tag has tini"
  docker run --rm --platform "$platform" --entrypoint tini "$image_tag" --version
}

test_tini_old_path() {
  echo "--- :hammer: Test $image_tag has tini executable at /sbin/tini"
  docker run --rm --platform "$platform" --entrypoint sh "$image_tag" -c '[ -x /sbin/tini ]'
}


# Test Cases

test_platform

case $variant in
  alpine | alpine-k8s | ubuntu-*)
    test_buildkite_agent
    test_docker_socket
    test_docker_compose
    test_docker_compose_v2
    test_docker_buildx
    test_tini
    test_tini_old_path
    ;;
  sidecar)
    test_buildkite_agent_sidecar
    ;;
  *)
    echo "Please add test cases for $variant"
    exit 1
    ;;
esac
