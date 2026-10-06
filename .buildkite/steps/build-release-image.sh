#!/usr/bin/env bash
set -euo pipefail

# Builds the image the release pipelines publish from, and records its ref in
# the "agent-release-tools-image" meta-data for upload-release-steps.sh.
#
# The tag is a hash of everything that goes into the image, so it's only
# rebuilt when one of those files changes, or after the ECR lifecycle policy
# expires it (14 days, like the other images in this repo).

inputs=(.buildkite/Dockerfile-release Gemfile Gemfile.lock)

hash="$(cat "${inputs[@]}" | sha256sum | cut -c1-16)"
image="445615400570.dkr.ecr.us-east-1.amazonaws.com/agent:release-tools-${hash}"

if docker buildx imagetools inspect "${image}" > /dev/null 2>&1; then
  echo "--- :docker: ${image} already exists"
else
  echo "--- :docker: Building ${image}"

  context="$(mktemp -d)"
  trap 'rm -rf "${context}"' EXIT
  cp "${inputs[@]}" "${context}"

  builder_name="$(docker buildx create --use)"
  trap 'rm -rf "${context}"; docker buildx rm "${builder_name}" || true' EXIT

  # Tags in this repo are immutable, so if another build pushed the same image
  # first, our push fails. That's fine as long as the image now exists.
  docker buildx build \
    --progress plain \
    --builder "${builder_name}" \
    --platform linux/amd64,linux/arm64 \
    --file "${context}/Dockerfile-release" \
    --tag "${image}" \
    --push \
    "${context}" \
    || docker buildx imagetools inspect "${image}" > /dev/null
fi

buildkite-agent meta-data set "agent-release-tools-image" "${image}"
