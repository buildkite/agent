#!/bin/bash
set -euo pipefail


echo "--- :package: Downloading bk binary"
curl -Lfs -o bk https://github.com/buildkite/cli/releases/download/v0.4.1/bk-linux-amd64-0.4.1
chmod +x ./bk

echo "--- :package: Downloading built binary"
rm -rf pkg/*
buildkite-agent artifact download pkg/buildkite-agent-linux-amd64 .
mv pkg/buildkite-agent-linux-amd64 pkg/buildkite-agent
chmod +x pkg/buildkite-agent

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT

BUILDKITE_AGENT_CONFIG="$tmpdir/buildkite-agent.cfg"
BUILDKITE_PLUGINS_PATH="$tmpdir/plugins"
BUILDKITE_HOOKS_PATH="$tmpdir/hooks"

touch "$BUILDKITE_AGENT_CONFIG"
mkdir -p "$BUILDKITE_PLUGINS_PATH" "$BUILDKITE_HOOKS_PATH"

env -i \
  BUILDKITE_AGENT_CONFIG="$BUILDKITE_AGENT_CONFIG" \
  BUILDKITE_PLUGINS_PATH="$BUILDKITE_PLUGINS_PATH" \
  BUILDKITE_HOOKS_PATH="$BUILDKITE_HOOKS_PATH" \
  PATH="$PWD/pkg:$PATH" \
  ./bk run --debug .buildkite/pipeline.bk-test.yml
