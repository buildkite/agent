#!/usr/bin/env bash
set -euo pipefail


echo "--- :package: Downloading bk binary"
go install github.com/buildkite/cli/v2/cmd/bk@cdcc5fa4b6e209f5ffa79469dad04938d6eed0cd

echo "--- :package: Downloading built binary"
rm -rf pkg/*
buildkite-agent artifact download pkg/buildkite-agent-linux-amd64 .
mv pkg/buildkite-agent-linux-amd64 pkg/buildkite-agent
chmod +x pkg/buildkite-agent

echo "--- :buildkite: Uploading a pipeline with bk cli as a backend"
export PATH="$PWD/pkg:$PATH"
bk run --debug .buildkite/pipeline.bk-test.yml
