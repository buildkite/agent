#!/usr/bin/env bash
set -euo pipefail

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

if [[ "${CODENAME:-}" == "" ]]; then
  echo "Error: Missing \$CODENAME (stable, experimental or unstable)"
  exit 1
fi

if [[ "${REGISTRY:-}" == "" ]]; then
    echo "Error: Missing \$REGISTRY (docker.io, ghcr.io, or packages.buildkite.com)"
    exit 1
fi

case "${REGISTRY}" in
docker.io)
  echo "--- docker login to Docker Hub"
  dockerhub_user="$(aws ssm get-parameter \
    --name /pipelines/agent/DOCKER_HUB_USER \
    --with-decryption \
    --output text \
    --query Parameter.Value \
    --region us-east-1\
  )"
  aws ssm get-parameter \
    --name /pipelines/agent/DOCKER_HUB_PASSWORD \
    --with-decryption \
    --output text \
    --query Parameter.Value \
    --region us-east-1 \
    | docker login --username="${dockerhub_user}" --password-stdin
  ;;
ghcr.io)
  echo "--- docker login to GitHub"

  ghcr_user=buildkite-agent-releaser
  aws ssm get-parameter \
    --name /pipelines/agent/GITHUB_RELEASE_ACCESS_TOKEN \
    --with-decryption \
    --output text \
    --query Parameter.Value \
    --region us-east-1 \
    | docker login ghcr.io --username="${ghcr_user}" --password-stdin

  ;;
packages.buildkite.com)
  echo "--- Uploading images to Buildkite Packages"
  ;;
*)
  echo "+++ Registry '${REGISTRY}' is not supported\!"
  exit 1
  ;;
esac

version="$(buildkite-agent meta-data get "agent-version")"
build="$(buildkite-agent meta-data get "agent-version-build")"

for variant in "alpine" "alpine-k8s" "ubuntu-20.04" "ubuntu-22.04" "ubuntu-24.04" "sidecar" ; do
  echo "--- Getting docker image tag for $variant from build meta data"
  source_image="$(buildkite-agent meta-data get "agent-docker-image-${variant}")"
  echo "Docker Image Tag for ${variant}: ${source_image}"

  echo "--- :docker: Publishing images for ${variant}"
  .buildkite/steps/publish-docker-image.sh "${REGISTRY}" "${variant}" "${source_image}" "${CODENAME}" "${version}" "${build}"
done
