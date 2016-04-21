#!/usr/bin/env bash

set -eu

curl \
  "https://api.buildkite.com/v2/organizations/buildkite/pipelines/docker-buildkite-agent/builds" \
  -X POST \
  --fail \
  -H "Authorization: Bearer ${DOCKER_RELEASE_BUILDKITE_API_TOKEN}" \
  -d "{
    \"commit\": \"HEAD\",
    \"branch\": \"master\",
    \"message\": \":rocket:\"
  }"

