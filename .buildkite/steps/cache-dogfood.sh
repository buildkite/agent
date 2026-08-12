#!/usr/bin/env bash
set -euo pipefail

echo "--- :inbox_tray: cache restore"
restore_start="$(date +%s)"
buildkite-agent cache restore --name gomodcache --name gocache
echo "CACHE_RESTORE_SECONDS=$(($(date +%s) - restore_start))"

echo "--- :go: go mod download + build"
build_start="$(date +%s)"
go mod download
go build ./...
echo "CACHE_BUILD_SECONDS=$(($(date +%s) - build_start))"

echo "--- :outbox_tray: cache save"
save_start="$(date +%s)"
buildkite-agent cache save --name gomodcache --name gocache
echo "CACHE_SAVE_SECONDS=$(($(date +%s) - save_start))"
