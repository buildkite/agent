#!/usr/bin/env bash
set -euo pipefail

# Keep the cache key and go install on the same pinned bk revision.
export BK_CLI_COMMIT="cdcc5fa4b6e209f5ffa79469dad04938d6eed0cd"
export BK_CLI_GOOS="$(go env GOOS)"
export BK_CLI_GOARCH="$(go env GOARCH)"
export GOBIN="$HOME/.bk-cli/bin"
export PATH="$GOBIN:$PATH"

mkdir -p "$GOBIN"

echo --- :inbox_tray: Restoring bk CLI
buildkite-agent cache restore --name bk_cli

if [[ ! -x "$GOBIN/bk" ]]; then
	echo "--- :package: Installing bk CLI"
	go install "github.com/buildkite/cli/v2/cmd/bk@${BK_CLI_COMMIT}"

	echo --- :outbox_tray: Saving bk CLI
	buildkite-agent cache save --name bk_cli
else
	echo "--- :package: Using cached bk CLI"
fi

echo "--- :package: Downloading built binary"
rm -rf pkg/*
buildkite-agent artifact download pkg/buildkite-agent-linux-amd64 .
mv pkg/buildkite-agent-linux-amd64 pkg/buildkite-agent
chmod +x pkg/buildkite-agent

echo "--- :buildkite: Uploading a pipeline with bk cli as a backend"
export PATH="$PWD/pkg:$PATH"
bk run --debug .buildkite/pipeline.bk-test.yml
