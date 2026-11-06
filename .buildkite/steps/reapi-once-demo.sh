#!/usr/bin/env bash
set -euo pipefail

echo '--- Once 0.77.0: verified upstream release'
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
archive=once-0.77.0-x86_64-unknown-linux-gnu.tar.gz
curl --fail --silent --show-error --location \
  "https://github.com/tuist/once/releases/download/0.77.0/$archive" \
  --output "$scratch/$archive"
echo "5bd10c35fdb0bffba04481368001146c00e0ef743ede7a96f5e36ceca24a522f  $scratch/$archive" | sha256sum --check
tar -xzf "$scratch/$archive" -C "$scratch"

mkdir -p once-live-proof
if [[ "$1" == hit ]]; then
  buildkite-agent artifact download once-live-proof/published.sha256 . --step once-publish
fi
echo '--- Compile PR REAPI server (no fixture, real Agent API and S3)'
git rev-parse HEAD
go build -o "$scratch/reapi-once-demo" ./.buildkite/reapi-once-demo
echo '--- Real Once build with independent empty local caches'
"$scratch/reapi-once-demo" "$scratch/once" "$1"
