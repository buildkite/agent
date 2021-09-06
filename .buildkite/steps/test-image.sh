#!/bin/bash

set -euo pipefail

image="$1"
platform="$2"

echo "--- :hammer: Testing $image $platform can run the buildkite-agent"
docker run --rm --platform "$platform" "$image" --version

echo "--- :hammer: Testing $image $platform can access docker socket"
docker run --rm --platform "$platform" --entrypoint "docker" -v /var/run/docker.sock:/var/run/docker.sock "$image" version

echo "--- :hammer: Testing $image $platform has docker-compose"
docker run --rm --platform "$platform" --entrypoint "docker-compose" "$image" version