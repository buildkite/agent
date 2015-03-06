#!/usr/bin/env bash

set -eu

curl --fail --data "build=true" -X POST "https://registry.hub.docker.com/u/buildkite/agent/trigger/$DOCKER_HUB_TRIGGER_TOKEN/"