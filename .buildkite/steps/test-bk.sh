#!/bin/bash
set -euxo pipefail

bk_cli_version=0.5.0

echo "--- :package: Downloading bk binary"
curl -Lfs -o bk "https://github.com/buildkite/cli/releases/download/v${bk_cli_version}/bk-linux-amd64-${bk_cli_version}"
chmod +x ./bk

echo "--- :package: Downloading built binary"
rm -rf pkg/*
buildkite-agent artifact download pkg/buildkite-agent-linux-amd64 .
mv pkg/buildkite-agent-linux-amd64 pkg/buildkite-agent
chmod +x pkg/buildkite-agent

export PATH="$PWD/pkg:$PATH"
./bk run --debug .buildkite/pipeline.bk-test.yml
