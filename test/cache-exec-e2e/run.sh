#!/usr/bin/env bash
# End-to-end tests for `buildkite-agent cache exec` (buildkite/agent PR #4436).
#
# Run from the repo root: ./test/cache-exec-e2e/run.sh
#
# Builds the agent from REPO (default: the git checkout containing this script;
# check out the PR branch first), starts fake_registry.py, and runs each case as
# a real `buildkite-agent bootstrap` job, so it has a job log redactor and Job API.
#
#   ok / FAIL   expected behaviour from the PR description
#   ISSUE       a known problem found in review, still reproduced
#   fixed?      a known problem that no longer reproduces
#
# Exits non-zero if any FAIL. Needs go, python3, curl, jq and zstd.
# KEEP=1 keeps the work directory (job logs are in $WORK/logs).
set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
REPO=${REPO:-$(git -C "$HERE" rev-parse --show-toplevel)}
WORK=$(mktemp -d)
PROJ=$WORK/proj
REG_LOG=$WORK/registry.log
JOB_LOG=$WORK/job.log
PASS=0 FAILS=0 ISSUES=0 STEP=0

cleanup() {
  [[ -n ${REG_PID:-} ]] && kill "$REG_PID" 2>/dev/null
  if [[ ${KEEP:-} == 1 || $FAILS -gt 0 ]]; then echo "work dir kept: $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

# --- setup -------------------------------------------------------------------

echo "Building agent from $REPO ($(git -C "$REPO" rev-parse --short HEAD))"
(cd "$REPO" && go build -o "$WORK/bin/buildkite-agent" .) || exit 1

python3 "$HERE/fake_registry.py" "$WORK/port" "$REG_LOG" &
REG_PID=$!
for _ in $(seq 50); do [[ -s $WORK/port ]] && break; sleep 0.1; done
BASE=http://127.0.0.1:$(cat "$WORK/port")

mkdir -p "$PROJ/.buildkite" "$PROJ/src" "$WORK/logs"
cat > "$PROJ/.buildkite/cache.yml" <<'EOF'
caches:
  - name: build
    cache_key:
      - build_v1
      - { checksum: ["src/**"] }
    target_paths:
      - dist
  - name: fb
    cache_key:
      - fb_v1
      - { env: FB_BRANCH, fallback_limit: true }
      - { checksum: ["src/**"] }
    target_paths:
      - dist
  - name: red
    cache_key:
      - red_v1
      - { env: RUN_ID }
    target_paths:
      - dist
  - name: badck
    cache_key:
      - badck_v1
      - { checksum: missing-lockfile.json }
    target_paths:
      - dist
  - name: ver
    cache_key:
      - ver_v1
      - { env: RUN_ID }
    target_paths:
      - verdist
EOF

cat > "$PROJ/build.sh" <<EOF
#!/bin/bash
echo run >> "$WORK/runs"
echo "BUILD RAN \$(date +%s\$RANDOM\$RANDOM)"
echo "to stderr: warning" >&2
mkdir -p dist && cp src/* dist/ && echo built > dist/out.txt
exit \${BUILD_EXIT:-0}
EOF

cat > "$PROJ/redact.sh" <<'EOF'
#!/bin/bash
echo "early print: late-secret-98765"
echo "late-secret-98765" | buildkite-agent redactor add 2>/dev/null
echo "env token: $MY_TOKEN"
echo "stderr token: $MY_TOKEN" >&2
echo "exported-in-step: $INLINE_SECRET"
echo "custom var: $CUSTOM_THING"
echo "pre-registered: prereg-value-77777"
echo "bk token: bkua_abcdefghijklmnopqrstuvwxyz0123456789ABCD"
echo "secret-get: $(buildkite-agent secret get MY_SECRET)"
echo "dynamic-redact-value-42" | buildkite-agent redactor add 2>/dev/null
echo "redactor-add: dynamic-redact-value-42"
printf 'split: dynamic-redact'; sleep 0.2; printf -- '-value-42 end\n'
printf -- '-----BEGIN KEY-----\nline-one-AAAAAAA\nline-two-BBBBBBB\n-----END KEY-----\n' > key.pem
buildkite-agent redactor add key.pem 2>/dev/null
echo "pem:"; cat key.pem
mkdir -p dist; echo ok > dist/x
EOF
chmod +x "$PROJ"/*.sh

agent_env=(
  BUILDKITE_AGENT_ACCESS_TOKEN=bkaa_faketoken123
  BUILDKITE_AGENT_ENDPOINT=$BASE/v3
  BUILDKITE_AGENT_CACHE_STORE_URL=file://$WORK/store
  BUILDKITE_BUILD_CHECKOUT_PATH=$PROJ
  PATH=$WORK/bin:$PATH
)

# --- helpers -----------------------------------------------------------------

# job <command>: runs <command> as a bootstrap job; log in $JOB_LOG, exit in $JOB_EXIT.
job() {
  STEP=$((STEP + 1))
  : > "$REG_LOG"
  (cd "$PROJ" && env "${agent_env[@]}" buildkite-agent bootstrap \
    --build-path="$WORK/builds" --job demo --phases command \
    --repository "$PROJ" --commit HEAD --branch main --pipeline-provider custom \
    --agent a --organization o --pipeline p --command "$1") > "$WORK/raw.log" 2>&1
  JOB_EXIT=$?
  perl -pe 's/\e\[[0-9;]*m//g' "$WORK/raw.log" > "$JOB_LOG"
  { cat "$JOB_LOG"; echo "--- registry requests:"; cat "$REG_LOG"; } > "$WORK/logs/$(printf %02d $STEP).log"
}

# direct [env...] -- <args>: runs cache exec outside any job (no Job API).
direct() {
  STEP=$((STEP + 1))
  (cd "$PROJ" && env -u BUILDKITE_AGENT_JOB_API_SOCKET -u BUILDKITE_AGENT_JOB_API_TOKEN "${agent_env[@]}" "$@") > "$WORK/raw.log" 2>&1
  JOB_EXIT=$?
  perl -pe 's/\e\[[0-9;]*m//g' "$WORK/raw.log" > "$JOB_LOG"
  cp "$JOB_LOG" "$WORK/logs/$(printf %02d $STEP).log"
}

section() { echo; echo "== $1"; }
expect() { local d=$1; shift; if "$@"; then echo "  ok     $d"; PASS=$((PASS + 1)); else echo "  FAIL   $d   (log: step $STEP)"; FAILS=$((FAILS + 1)); fi; }
issue() { local d=$1; shift; if "$@"; then echo "  ISSUE  $d"; ISSUES=$((ISSUES + 1)); else echo "  fixed? $d (no longer reproduces)"; fi; }
has() { grep -qF -- "$1" "$JOB_LOG"; }
lacks() { ! has "$1"; }
count() { grep -cF -- "$1" "$JOB_LOG"; }
reg_has() { grep -qE -- "$1" "$REG_LOG"; }
runs() { if [[ -f $WORK/runs ]]; then tr -cd '\n' < "$WORK/runs" | wc -c | tr -d ' '; else echo 0; fi; }
newsrc() { echo "$1-$RANDOM$RANDOM" > "$PROJ/src/$1.txt"; rm -rf "$PROJ/dist"; }
# sidecar_digest: blob digest of the most recently committed output entry.
sidecar_digest() { curl -s "$BASE/control/entries" | jq -r '[.[] | select(.target_paths | index("<cache-exec-output>"))][-1].blobs[0].digest.value'; }
# decode_sidecar <blob>: prints the recorded stdout and stderr bytes, without the frame headers.
decode_sidecar() {
  zstd -dcq "$1" | python3 -c '
import sys
data = sys.stdin.buffer.read()
header = b"buildkite-agent cache exec output v1\n"
assert data.startswith(header), "unrecognised recording"
i, out = len(header), sys.stdout.buffer
while i < len(data):
    i += 1  # stream byte
    n = shift = 0
    while True:
        b = data[i]; i += 1
        n |= (b & 0x7F) << shift; shift += 7
        if b < 0x80: break
    out.write(data[i:i + n]); i += n
'
}

# --- cases -------------------------------------------------------------------

section "miss, then hit"
newsrc s1
job 'buildkite-agent cache exec --name build -- ./build.sh'
expect "miss runs the command" has "No cached result, running command"
expect "miss saves the files and the output" has "Saving command output for cache: build"
recorded=$(grep -o 'BUILD RAN [0-9]*' "$JOB_LOG")
rm -rf "$PROJ/dist"; before=$(runs)
job "buildkite-agent cache exec --name build -- ./build.sh >$WORK/stdout 2>$WORK/stderr"
expect "hit skips the command" test "$(runs)" -eq "$before"
expect "hit says it is replaying" grep -q "Replaying output from cache" "$WORK/stdout"
expect "hit replays the recorded stdout" grep -qF "$recorded" "$WORK/stdout"
expect "hit replays stderr to stderr" grep -q "to stderr: warning" "$WORK/stderr"
expect "hit restores target_paths" test -f "$PROJ/dist/out.txt"

section "non-zero exit"
newsrc s2
job 'BUILD_EXIT=3 buildkite-agent cache exec --name build -- ./build.sh || echo "EXIT=$?"'
expect "exit status is passed through" has "EXIT=3"
expect "nothing is saved" bash -c "! grep -qE '^(peek|store|commit)' '$REG_LOG'"

section "plain cache restore of an exec-saved cache"
newsrc s3
job 'buildkite-agent cache exec --name build -- ./build.sh'
rm -rf "$PROJ/dist"
job 'buildkite-agent cache restore --name build && ls dist'
expect "restores target_paths" has "out.txt"
expect "does not replay output" lacks "Replaying output"
expect "never asks for the output entry" bash -c "! grep -q 'cache-exec-output' '$REG_LOG'"

section "fallback_limit is ignored"
newsrc s4
job 'FB_BRANCH=main buildkite-agent cache exec --name fb -- ./build.sh'
newsrc s4b
job 'FB_BRANCH=main buildkite-agent cache exec --name fb -- ./build.sh'
expect "logs that fallback_limit is ignored" has "ignores fallback_limit"
expect "runs the command instead of using the fallback" has "No cached result, running command"
expect "sends every key part as mandatory" bash -c "grep -q '^retrieve' '$REG_LOG' && ! grep -E '^retrieve' '$REG_LOG' | grep -qF '?'"

section "files from plain cache save are never paired with output"
newsrc s5; mkdir -p "$PROJ/dist"; echo stale > "$PROJ/dist/out.txt"
job 'buildkite-agent cache save --name build && buildkite-agent cache exec --name build -- ./build.sh && buildkite-agent cache exec --name build -- ./build.sh'
expect "exec runs the command over plain-saved files" has "no recorded command output for these files"
expect "no output is replayed" lacks "Replaying output"
issue "after a plain save, that key never caches again (both execs ran)" test "$(count 'No cached result, running command')" -eq 2

section "cache save --force replacing exec-saved files"
newsrc s6
job 'buildkite-agent cache exec --name build -- ./build.sh >/dev/null 2>&1 && echo tampered > dist/out.txt && buildkite-agent cache save --force --name build && rm -rf dist && buildkite-agent cache exec --name build -- ./build.sh'
expect "old output isn't replayed with the replaced files" lacks "Replaying output"
expect "the command runs" has "No cached result, running command"

section "corrupt output entry"
newsrc s7
job 'buildkite-agent cache exec --name build -- ./build.sh'
echo garbage > "$WORK/store/$(sidecar_digest)"
rm -rf "$PROJ/dist"
job 'buildkite-agent cache exec --name build -- ./build.sh && buildkite-agent cache exec --name build -- ./build.sh'
expect "corrupt output is treated as a miss" has "missing or corrupt"
expect "corrupt output entry is expired" reg_has '^expire.*cache-exec-output'
issue "after the output entry is expired, that key never caches again (both execs ran)" test "$(count 'No cached result, running command')" -eq 2

section "saved output redaction"
export MY_TOKEN=envtokenvalue-11111
job 'export BUILDKITE_JOB_ID=demo INLINE_SECRET=inline-secret-33333 CUSTOM_THING=custom-value-55555
echo prereg-value-77777 | buildkite-agent redactor add 2>/dev/null
RUN_ID=red1 buildkite-agent cache exec --name red --redacted-vars "*_TOKEN,*_SECRET,CUSTOM_THING" -- ./redact.sh'
unset MY_TOKEN
decode_sidecar "$WORK/store/$(sidecar_digest)" > "$WORK/sidecar.txt"
expect "secret get worked in the job" grep -qaF "secret-get: [REDACTED]" "$WORK/sidecar.txt"
for s in envtokenvalue-11111 inline-secret-33333 custom-value-55555 prereg-value-77777 \
  bkua_abcdefghijklmnopqrstuvwxyz0123456789ABCD s3cr3t-from-secret-get-XYZ \
  dynamic-redact-value-42 late-secret-98765 line-one-AAAAAAA; do
  expect "saved output doesn't contain $s" bash -c "! grep -qaF '$s' '$WORK/sidecar.txt'"
done
job 'RUN_ID=red1 buildkite-agent cache exec --name red -- ./redact.sh'
expect "redacted output replays in a later job" has "split: [REDACTED] end"

section "cache key resolution failure"
job 'buildkite-agent cache exec --name badck -- ./build.sh || echo "EXIT=$?"
buildkite-agent cache exec --cache-fail-on-error --name badck -- ./build.sh || echo "FOE_EXIT=$?"'
expect "runs the command uncached" has "running the command without caching"
expect "the command ran" has "BUILD RAN"
expect "--cache-fail-on-error fails instead" has "FOE_EXIT=1"

section "Job API unreachable"
newsrc s8
direct buildkite-agent cache exec --name build -- ./build.sh
expect "outside a job, runs the command uncached" has "running the command without caching"
expect "and exits 0" test "$JOB_EXIT" -eq 0
direct BUILDKITE_AGENT_JOB_API_SOCKET=/nonexistent.sock BUILDKITE_AGENT_JOB_API_TOKEN=x buildkite-agent cache exec --cache-fail-on-error --name build -- ./build.sh
expect "--cache-fail-on-error fails on a dead Job API socket" test "$JOB_EXIT" -eq 1
direct RUN_ID=red1 buildkite-agent cache exec --name red -- ./redact.sh
issue "without the Job API, an existing hit is ignored and the command runs" has "running the command without caching"

section "other known issues"
job 'mkdir -p verdist; V=$(RUN_ID=cap1 buildkite-agent cache exec --name ver -- echo 1.2.3 2>/dev/null); echo "CAPTURED=[$V]"'
issue "stdout includes section headers, so \$(cache exec -- echo 1.2.3) captures them" has "CAPTURED=[--- :package:"
job 'RUN_ID=nl1 buildkite-agent cache exec --name red -- bash -c "mkdir -p dist; printf no-newline"'
issue "the Saving header is glued to output that lacks a trailing newline" has "no-newline--- :package: Saving cache..."
start=$SECONDS
job 'RUN_ID=bg1 buildkite-agent cache exec --name red -- bash -c "mkdir -p dist; (sleep 5; echo late-from-bg) & echo started"'
issue "exec waits for a background child holding stdout ($((SECONDS - start))s for a command that exits at once)" test $((SECONDS - start)) -ge 5
job 'RUN_ID=sig1 buildkite-agent cache exec --name red -- bash -c "kill -9 \$\$" || echo "EXIT=$?"'
issue "a child killed by SIGKILL makes exec exit 1, not 137" has "EXIT=1"
job 'buildkite-agent cache exec --name nope -- echo "RAN-$((40 + 2))" || echo "EXIT=$?"'
issue "an unknown --name fails without running the command" lacks "RAN-42"

echo
echo "passed: $PASS  failed: $FAILS  known issues reproduced: $ISSUES"
[[ $FAILS -eq 0 ]]
