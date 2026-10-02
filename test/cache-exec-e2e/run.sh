#!/usr/bin/env bash
# End-to-end tests for `buildkite-agent cache exec`.
#
# Usage, from the repo root: ./test/cache-exec-e2e/run.sh
# Needs go, python3, curl and zstd. No Buildkite token or network access is needed.
#
# Builds the agent, starts a fake cache registry (fake_registry.py) backed by a local
# file store, and runs each case as a real `buildkite-agent bootstrap` job, so commands
# get a real job log redactor and Job API.
#
# Results:
#   ok / FAIL   behaviour cache exec promises; any FAIL makes the script exit non-zero
#   ISSUE       a known limitation that still happens
#   fixed?      a known limitation that no longer happens
set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../.." && pwd)
WORK=$(mktemp -d)
PROJECT=$WORK/project
LOG=$WORK/output.log            # output of the last in_job or outside_job
REQUESTS=$WORK/requests.log     # registry requests made by the last in_job or outside_job
PASSED=0 FAILED=0 ISSUES=0

trap 'kill "${REGISTRY_PID:-}" 2>/dev/null; rm -rf "$WORK"' EXIT

# --- Setup ---------------------------------------------------------------------

echo "Building the agent"
# -buildvcs=false: CI mounts the checkout into a container as another user, and git refuses to read it.
go -C "$REPO" build -buildvcs=false -o "$WORK/bin/buildkite-agent" . || exit 1

python3 "$HERE/fake_registry.py" "$WORK/port" "$REQUESTS" &
REGISTRY_PID=$!
for _ in {1..50}; do [[ -s $WORK/port ]] && break; sleep 0.1; done
REGISTRY=http://127.0.0.1:$(cat "$WORK/port")

AGENT_ENV=(
  PATH="$WORK/bin:$PATH"
  BUILDKITE_AGENT_ACCESS_TOKEN=bkaa_fake
  BUILDKITE_AGENT_ENDPOINT="$REGISTRY/v3"
  BUILDKITE_AGENT_CACHE_STORE_URL="file://$WORK/store"
  BUILDKITE_AGENT_NO_COLOR=true
  BUILDKITE_BUILD_CHECKOUT_PATH="$PROJECT"
  BUILDKITE_JOB_ID=demo
)

mkdir -p "$PROJECT/.buildkite" "$PROJECT/src"
touch "$WORK/runs"
cd "$PROJECT" || exit 1

cat > .buildkite/cache.yml <<'EOF'
caches:
  - name: build             # keyed on the source files
    cache_key: [build, { checksum: "src/**" }]
    target_paths: [dist]
  - name: with_fallback     # declares fallback_limit, which cache exec ignores
    cache_key: [with_fallback, { env: RUN_ID, fallback_limit: true }, { checksum: "src/**" }]
    target_paths: [dist]
  - name: by_run_id         # keyed on $RUN_ID, so each case picks its own key
    cache_key: [by_run_id, { env: RUN_ID }]
    target_paths: [dist]
  - name: broken_key        # its checksum input doesn't exist
    cache_key: [broken_key, { checksum: missing.lock }]
    target_paths: [dist]
EOF

cat > build.sh <<'EOF'
#!/bin/bash
# Counts its runs, prints a line unique to this run, and copies src/ to dist/.
echo run >> ../runs
echo "BUILD RAN $RANDOM$RANDOM$RANDOM"
echo "build warning" >&2
mkdir -p dist && cp src/* dist/
exit "${BUILD_EXIT:-0}"
EOF

cat > redact.sh <<'EOF'
#!/bin/bash
# Prints secrets that reach the redactor in different ways. Every secret starts with "SECRET-".
echo "printed before it's registered: SECRET-registered-late"
echo SECRET-registered-late | buildkite-agent redactor add 2>/dev/null
echo "agent environment variable: $MY_TOKEN"
echo "agent environment variable on stderr: $MY_TOKEN" >&2
echo "exported in the step: $STEP_SECRET"
echo "custom --redacted-vars pattern: $CUSTOM_THING"
echo "registered before cache exec: SECRET-registered-before"
echo "Buildkite token: bkua_SECRET-abcdefghijklmnopqrstuvwxyz"
echo "secret get: $(buildkite-agent secret get MY_SECRET)"
echo SECRET-registered-during | buildkite-agent redactor add 2>/dev/null
echo "registered while running: SECRET-registered-during"
printf 'split across writes: SECRET-registered-'; sleep 0.2; printf 'during\n'
printf 'SECRET-key-line-1\nSECRET-key-line-2\n' > key.pem
buildkite-agent redactor add key.pem 2>/dev/null
cat key.pem
mkdir -p dist
EOF
chmod +x build.sh redact.sh

# --- Helpers -------------------------------------------------------------------

# in_job <command>: runs <command> as a pipeline step would, in a bootstrap job.
in_job() {
  : > "$REQUESTS"
  env "${AGENT_ENV[@]}" buildkite-agent bootstrap --phases command --command "$1" \
    --build-path "$WORK/builds" --job demo --repository "$PROJECT" --commit HEAD --branch main \
    --agent a --organization o --pipeline p --pipeline-provider custom > "$LOG" 2>&1
}

# outside_job [VAR=value...] <command...>: runs a command with no job and no Job API; sets $EXIT.
outside_job() {
  : > "$REQUESTS"
  env -u BUILDKITE_AGENT_JOB_API_SOCKET -u BUILDKITE_AGENT_JOB_API_TOKEN "${AGENT_ENV[@]}" "$@" > "$LOG" 2>&1
  EXIT=$?
}

section() { printf '\n== %s\n' "$1"; }

# expect <description> <check...>: reports ok or FAIL; on FAIL, shows the last output and registry requests.
expect() {
  local description=$1; shift
  if "$@"; then
    echo "  ok     $description"; PASSED=$((PASSED + 1))
  else
    echo "  FAIL   $description"; FAILED=$((FAILED + 1))
    { tail -n 40 "$LOG"; echo "--- registry requests:"; cat "$REQUESTS"; } | sed 's/^/         | /'
  fi
}

# issue <description> <check...>: the check succeeds while the known problem still happens.
issue() {
  local description=$1; shift
  if "$@"; then
    echo "  ISSUE  $description"; ISSUES=$((ISSUES + 1))
  else
    echo "  fixed? $description"
  fi
}

not() { ! "$@"; }
has() { grep -qF -- "$1" "$LOG"; }                  # the last output contains this text
requested() { grep -qE -- "$1" "$REQUESTS"; }       # the registry got a request matching this regex
file_has() { grep -qaF -- "$2" "$1"; }
ran_twice() { test "$(grep -c 'No cached result, running command' "$LOG")" -eq 2; }
runs() { wc -l < "$WORK/runs"; }

# new_key: changes the source files, so the build cache key has nothing saved yet.
new_key() { echo "$RANDOM$RANDOM" > src/input.txt; rm -rf dist; }

# saved_output_blob: path of the newest saved command output in the file store.
saved_output_blob() { echo "$WORK/store/$(curl -s "$REGISTRY/control/latest-output")"; }

# decode_saved_output <blob>: prints the recorded output, without the format's framing.
decode_saved_output() {
  zstd -dcq "$1" | python3 -c '
import sys
data = sys.stdin.buffer.read()
i = len(b"buildkite-agent cache exec output v1\n")
while i < len(data):
    i += 1  # stream: 1 = stdout, 2 = stderr
    size = shift = 0
    while True:  # uvarint length
        byte = data[i]; i += 1
        size |= (byte & 0x7F) << shift; shift += 7
        if byte < 0x80: break
    sys.stdout.buffer.write(data[i:i + size]); i += size
'
}

# --- Cases ---------------------------------------------------------------------

section "miss, then hit"
new_key
in_job 'buildkite-agent cache exec --name build -- ./build.sh'
expect "a miss runs the command" has "No cached result, running command"
expect "and saves the files and the output" has "Saving command output for cache: build"
first_run=$(grep -o 'BUILD RAN [0-9]*' "$LOG")
rm -rf dist
runs_before=$(runs)
in_job 'buildkite-agent cache exec --name build -- ./build.sh > ../stdout 2> ../stderr'
expect "a hit skips the command" test "$(runs)" -eq "$runs_before"
expect "and restores target_paths" test -f dist/input.txt
expect "and replays the recorded stdout" file_has "$WORK/stdout" "$first_run"
expect "and replays stderr to stderr" file_has "$WORK/stderr" "build warning"

section "a failing command"
new_key
in_job 'BUILD_EXIT=3 buildkite-agent cache exec --name build -- ./build.sh || echo "EXIT=$?"'
expect "its exit status is passed through" has "EXIT=3"
expect "nothing is saved" not requested '^(peek|store|commit) '

section "plain cache restore of an exec-saved cache"
new_key
in_job 'buildkite-agent cache exec --name build -- ./build.sh'
rm -rf dist
in_job 'buildkite-agent cache restore --name build'
expect "restores target_paths" test -f dist/input.txt
expect "without replaying output" not has "Replaying output"
expect "or asking the registry for it" not requested 'cache-exec-output'

section "fallback_limit is ignored"
# The second run's source checksum differs, so only a fallback match could hit.
new_key
in_job 'RUN_ID=fallback buildkite-agent cache exec --name with_fallback -- ./build.sh'
new_key
in_job 'RUN_ID=fallback buildkite-agent cache exec --name with_fallback -- ./build.sh'
expect "cache exec says it ignores fallback_limit" has "ignores fallback_limit"
expect "and runs the command" has "No cached result, running command"
expect "every key part is sent as mandatory" not requested '^retrieve .*[?]'

section "identical files from a plain cache save"
new_key
./build.sh > /dev/null 2>&1
in_job 'buildkite-agent cache save --name build
buildkite-agent cache exec --name build -- ./build.sh
buildkite-agent cache exec --name build -- ./build.sh'
expect "the first exec saves output for them" has "already holds this run's files"
expect "and the second is a hit" has "Replaying output from cache"

section "different files from a plain cache save"
new_key
mkdir -p dist && echo stale > dist/input.txt
in_job 'buildkite-agent cache save --name build
buildkite-agent cache exec --name build -- ./build.sh
buildkite-agent cache exec --name build -- ./build.sh'
expect "aren't paired with output" has "no recorded command output for these files"
expect "and no output is replayed" not has "Replaying output"
issue "the key never caches again until the plain-saved entry expires (both execs ran)" ran_twice

section "exec-saved files replaced by cache save --force"
new_key
in_job 'buildkite-agent cache exec --name build -- ./build.sh
echo replaced > dist/input.txt
buildkite-agent cache save --force --name build
rm -rf dist
buildkite-agent cache exec --name build -- ./build.sh'
expect "the old output isn't replayed with the new files" not has "Replaying output"
expect "the command runs" ran_twice

section "corrupt saved output"
new_key
in_job 'buildkite-agent cache exec --name build -- ./build.sh'
echo garbage > "$(saved_output_blob)"
rm -rf dist
in_job 'buildkite-agent cache exec --name build -- ./build.sh
buildkite-agent cache exec --name build -- ./build.sh'
expect "is treated as a miss" has "missing or corrupt"
expect "and expired" requested '^expire .*cache-exec-output'
expect "the next run saves new output for the same files" has "already holds this run's files"
expect "and the run after that is a hit" has "Replaying output from cache"

section "saved output is redacted"
MY_TOKEN=SECRET-agent-env in_job 'echo SECRET-registered-before | buildkite-agent redactor add 2>/dev/null
export STEP_SECRET=SECRET-step-env CUSTOM_THING=SECRET-custom-var
RUN_ID=redact buildkite-agent cache exec --name by_run_id --redacted-vars "*_TOKEN,*_SECRET,CUSTOM_THING" -- ./redact.sh'
decode_saved_output "$(saved_output_blob)" > "$WORK/saved-output"
expect "secret get worked" file_has "$WORK/saved-output" "secret get: [REDACTED]"
expect "the saved output contains no secrets" not file_has "$WORK/saved-output" "SECRET-"
in_job 'RUN_ID=redact buildkite-agent cache exec --name by_run_id -- ./redact.sh'
expect "a later job replays the redacted output" has "split across writes: [REDACTED]"

section "a cache key that can't be computed"
in_job 'buildkite-agent cache exec --name broken_key -- ./build.sh'
expect "runs the command uncached" has "running the command without caching"
expect "and the command runs" has "BUILD RAN"
in_job 'buildkite-agent cache exec --cache-fail-on-error --name broken_key -- ./build.sh || echo "EXIT=$?"'
expect "--cache-fail-on-error fails instead" has "EXIT=1"

section "no Job API"
new_key
outside_job buildkite-agent cache exec --name build -- ./build.sh
expect "a miss runs the command" has "BUILD RAN"
expect "and exits 0" test "$EXIT" -eq 0
expect "but saves nothing" not requested '^(peek|store|commit) '
outside_job RUN_ID=redact buildkite-agent cache exec --name by_run_id -- false
expect "a saved result is still replayed" has "Replaying output from cache"
outside_job BUILDKITE_AGENT_JOB_API_SOCKET=/nonexistent.sock BUILDKITE_AGENT_JOB_API_TOKEN=x \
  buildkite-agent cache exec --cache-fail-on-error --name build -- ./build.sh
expect "--cache-fail-on-error fails when the Job API socket is dead" test "$EXIT" -eq 1

section "output and exit status"
in_job 'mkdir -p dist
for run in miss hit; do echo "$run=[$(RUN_ID=capture buildkite-agent cache exec --name by_run_id -- echo 1.2.3 2>/dev/null)]"; done'
expect "on a miss, \$(cache exec -- echo 1.2.3) captures only 1.2.3" has "miss=[1.2.3]"
expect "and on a hit" has "hit=[1.2.3]"
in_job 'RUN_ID=no_newline buildkite-agent cache exec --name by_run_id -- bash -c "mkdir -p dist; printf no-newline"'
expect "a header after output without a trailing newline starts on a new line" grep -q -- '^--- :package: Saving cache' "$LOG"
start=$SECONDS
in_job 'RUN_ID=background buildkite-agent cache exec --name by_run_id -- bash -c "mkdir -p dist; (sleep 5; echo late) & echo started"'
expect "cache exec doesn't wait for a background process holding the output open" test $((SECONDS - start)) -lt 4
in_job 'RUN_ID=sigkill buildkite-agent cache exec --name by_run_id -- bash -c "kill -9 \$\$" || echo "EXIT=$?."
buildkite-agent cache exec --name by_run_id -- ./does-not-exist || echo "EXIT=$?."'
expect "a command killed by SIGKILL exits 137" has "EXIT=137."
expect "a command that can't be started exits 127" has "EXIT=127."

section "missing cache configuration"
# $((40 + 2)) so the job log's echo of the command line doesn't match "RAN-42".
in_job 'buildkite-agent cache exec --name unknown -- echo "RAN-$((40 + 2))"
BUILDKITE_CACHE_CONFIG_FILE=missing.yml buildkite-agent cache exec --name build -- echo "RAN-$((40 + 3))"
buildkite-agent cache exec --cache-fail-on-error --name unknown -- true || echo "EXIT=$?."'
expect "an unknown --name runs the command uncached" has "RAN-42"
expect "a missing config file runs the command uncached" has "RAN-43"
expect "--cache-fail-on-error fails instead" has "EXIT=1."

printf '\npassed: %d  failed: %d  known issues: %d\n' "$PASSED" "$FAILED" "$ISSUES"
[[ $FAILED -eq 0 ]]
