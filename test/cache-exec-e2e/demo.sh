#!/usr/bin/env bash
# Demo of the time `buildkite-agent cache exec` saves.
#
# Usage, from the repo root: ./test/cache-exec-e2e/demo.sh
# Needs go and python3. No Buildkite token or network access is needed.
#
# Runs one slow build step three times, each as a real `buildkite-agent bootstrap` job,
# against a fake cache registry (fake_registry.py) backed by a local file store:
#   1. first run:          no cached result, so the build runs and is saved
#   2. same sources:       cached, so the build is skipped and its output replayed
#   3. a source changed:   new cache key, so the build runs again
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../.." && pwd)
WORK=$(mktemp -d)
PROJECT=$WORK/project
trap 'kill "${REGISTRY_PID:-}" 2>/dev/null; rm -rf "$WORK"' EXIT

echo "~~~ :hammer: Setup: building the agent and starting a fake cache registry"

# -buildvcs=false: CI mounts the checkout into a container as another user, and git refuses to read it.
go -C "$REPO" build -buildvcs=false -o "$WORK/bin/buildkite-agent" .

python3 "$HERE/fake_registry.py" "$WORK/port" /dev/null &
REGISTRY_PID=$!
for _ in {1..50}; do [[ -s $WORK/port ]] && break; sleep 0.1; done

mkdir -p "$PROJECT/.buildkite" "$PROJECT/src"
cd "$PROJECT"
echo "export const greeting = 'hello'" > src/app.js

# The cache key is a checksum of the sources, so the build is skipped only while they're unchanged.
cat > .buildkite/cache.yml <<'EOF'
caches:
  - name: frontend_build
    cache_key:
      - frontend_build
      - { checksum: "src/**" }
    target_paths:
      - dist
EOF

# A stand-in for a slow frontend build: about 15 seconds, then copies src/ to dist/.
cat > slow-build.sh <<'EOF'
#!/bin/bash
echo "vite v5.4.0 building for production..."
for step in "transforming modules" "rendering chunks" "minifying" "computing gzip size" "writing dist/"; do
  sleep 3
  echo "  $step..."
done
mkdir -p dist && cp src/* dist/
echo "✓ built in 15s"
EOF
chmod +x slow-build.sh

echo "Cache config (.buildkite/cache.yml):"
sed 's/^/    /' .buildkite/cache.yml

# step <title>: runs the build step in a bootstrap job, as a pipeline step would, and times it.
step() {
  echo "+++ $1"
  local start=$SECONDS
  env PATH="$WORK/bin:$PATH" \
    BUILDKITE_AGENT_ACCESS_TOKEN=bkaa_fake \
    BUILDKITE_AGENT_ENDPOINT="http://127.0.0.1:$(cat "$WORK/port")/v3" \
    BUILDKITE_AGENT_CACHE_STORE_URL="file://$WORK/store" \
    BUILDKITE_AGENT_NO_COLOR=true \
    BUILDKITE_BUILD_CHECKOUT_PATH="$PROJECT" \
    buildkite-agent bootstrap --phases command \
    --command 'buildkite-agent cache exec --name frontend_build -- ./slow-build.sh' \
    --build-path "$WORK/builds" --job demo --repository "$PROJECT" --commit HEAD --branch main \
    --agent a --organization o --pipeline p --pipeline-provider custom 2>&1 |
    # Drop bootstrap's own log group so cache exec's groups show; unbuffered so log timestamps are real.
    sed -u 's/\r$//' | grep --line-buffered -v -e 'Running commands$' -e ' cd /'
  local took=$((SECONDS - start))
  echo ":stopwatch: This step took ${took}s. dist/ contains: $(ls dist)"
  TIMES+=("$(printf '%4ss  %s' "$took" "$1")")
  rm -rf dist # so the next run has to build or restore it
}

TIMES=()
step ":one: First run: no cached result, so the build runs and is saved"
step ":two: Same sources: the build is skipped and its output replayed"
echo "export const greeting = 'hello, world'" > src/app.js
step ":three: A source file changed: new cache key, so the build runs again"

echo "+++ :bar_chart: Summary"
printf '  %s\n' "${TIMES[@]}"
