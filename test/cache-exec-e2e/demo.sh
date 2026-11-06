#!/usr/bin/env bash
# Demo: the time `buildkite-agent cache exec` saves.
#
# The same slow build step runs three times:
#   1. First run          nothing is cached yet, so the build runs and is saved
#   2. Nothing changed    the build is skipped; its files and log are restored
#   3. A file changed     the cache no longer matches, so the build runs again
#
# Run from the repo root: ./test/cache-exec-e2e/demo.sh
# It uses a fake local cache (fake_registry.py), so no Buildkite token is needed.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../.." && pwd)
WORK=$(mktemp -d)
trap 'kill "${REGISTRY_PID:-}" 2>/dev/null; rm -rf "$WORK"' EXIT

# ---------------------------------------------------------------------------
# Setup: build the agent, start the fake cache, and create a small project.
# ---------------------------------------------------------------------------

echo "~~~ Setup"

go -C "$REPO" build -buildvcs=false -o "$WORK/bin/buildkite-agent" .

python3 "$HERE/fake_registry.py" "$WORK/port" /dev/null &
REGISTRY_PID=$!
for _ in {1..50}; do [[ -s $WORK/port ]] && break; sleep 0.1; done

mkdir -p "$WORK/project/.buildkite" "$WORK/project/src"
cd "$WORK/project"

echo "export const greeting = 'hello'" > src/app.js

# The cache is keyed on the contents of src/, and saves the dist/ folder.
cat > .buildkite/cache.yml <<'EOF'
caches:
  - name: frontend_build
    cache_key:
      - frontend_build
      - { checksum: "src/**" }
    target_paths:
      - dist
EOF

# A stand-in for a slow build: takes 15 seconds, then writes dist/.
cat > build.sh <<'EOF'
#!/bin/bash
echo "Building..."
for step in "compiling" "bundling" "minifying" "compressing" "writing dist/"; do
  sleep 3
  echo "  $step"
done
mkdir -p dist && cp src/* dist/
echo "Done in 15s"
EOF
chmod +x build.sh

# ---------------------------------------------------------------------------
# run_step <title>: runs the build as a real Buildkite job step, and times it.
# ---------------------------------------------------------------------------

STEP_COMMAND="buildkite-agent cache exec --name frontend_build -- ./build.sh"
SUMMARY=""

run_step() {
  echo "+++ $1"
  rm -rf dist
  local start=$SECONDS

  env PATH="$WORK/bin:$PATH" \
    BUILDKITE_AGENT_ACCESS_TOKEN=fake-token \
    BUILDKITE_AGENT_ENDPOINT="http://127.0.0.1:$(cat "$WORK/port")/v3" \
    BUILDKITE_AGENT_CACHE_STORE_URL="file://$WORK/store" \
    BUILDKITE_BUILD_CHECKOUT_PATH="$WORK/project" \
    buildkite-agent bootstrap --phases command --command "$STEP_COMMAND" \
      --job demo --build-path "$WORK/builds" --repository . --commit HEAD --branch main \
      --agent a --organization o --pipeline p --pipeline-provider custom 2>&1 |
    tidy_log

  local took=$((SECONDS - start))
  echo ":stopwatch: Took ${took}s"
  SUMMARY+=$(printf '  %-26s %3ss' "$1" "$took")$'\n'
}

# tidy_log: removes the job's own setup lines and the fake cache's notes, and
# passes everything else straight through, line by line, so log times are real.
tidy_log() {
  sed -u -e 's/\r$//' -e 's/ (search diagnostics unavailable)//' -e 's/ from unscoped//' |
    grep --line-buffered -v -e 'Running commands$' -e ' cd /'
}

# ---------------------------------------------------------------------------
# The demo
# ---------------------------------------------------------------------------

run_step "1. First run"

run_step "2. Nothing changed"

echo "export const greeting = 'hello, world'" > src/app.js
run_step "3. A source file changed"

echo "+++ :bar_chart: Summary"
echo "  Step command: $STEP_COMMAND"
echo
printf '%s' "$SUMMARY"
