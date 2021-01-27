#!/bin/bash
set -e

echo '--- Getting agent version from build meta data'

image_tag=$(buildkite-agent meta-data get "agent-docker-image-alpine")

echo "Docker Alpine Image Tag: $image_tag"

rm -rf pkg
mkdir -p pkg

echo '--- Downloading :linux: binaries'
buildkite-agent artifact download "pkg/buildkite-agent-linux-amd64" .

echo '--- Building :docker: image'
cp pkg/buildkite-agent-linux-amd64 packaging/docker/linux/buildkite-agent
chmod +x packaging/docker/linux/buildkite-agent
docker build --tag "$image_tag" packaging/docker/linux

echo "--- :hammer: Testing $image_tag can run"
docker run --rm --entrypoint "buildkite-agent" "$image_tag" --version

echo "--- :hammer: Testing $image_tag can access docker socket"
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock --entrypoint "docker" "$image_tag" version

echo "--- :hammer: Testing $image_tag has docker-compose"
docker run --rm --entrypoint "docker-compose" "$image_tag" --version

echo '--- Pushing :docker: image to buildkiteci/agent'
docker push "$image_tag"
