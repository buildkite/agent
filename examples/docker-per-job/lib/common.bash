# Shared by the docker-per-job hooks. Source it; don't run it.

dpj_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Settings: BK_DPJ_CONFIG (from the agent's environment) or the default path.
# shellcheck disable=SC1090
source "${BK_DPJ_CONFIG:-/etc/buildkite-agent/docker-per-job/docker-per-job.env}"
export BK_DPJ_KEEP_FILE BK_DPJ_CONTEXT_DIR BK_DPJ_JOBS_ROOT
export BK_DPJ_HELPER_IMAGE="${BK_DPJ_HELPER_IMAGE:-$BK_DPJ_IMAGE}"

dpj_poison_file="$BK_DPJ_STATE_DIR/POISONED"

dpj_log() { echo "docker-per-job: $*" >&2; }

dpj_reap() {
  "$dpj_root/bin/docker-per-job-reap"
}

# Prints the PID of the `buildkite-agent start` process this hook runs under.
dpj_agent_pid() {
  local pid=$$ ppid
  while [[ "$pid" -gt 1 ]]; do
    if tr '\0' ' ' <"/proc/$pid/cmdline" | grep -Eq '(^|/)buildkite-agent( .*)? start( |$)'; then
      echo "$pid"
      return 0
    fi
    ppid="$(awk '{print $4}' "/proc/$pid/stat")"
    pid="$ppid"
  done
  return 1
}

# Marks this host unusable and stops the agent, so it doesn't keep accepting
# jobs and failing them. agent-startup refuses to start while POISONED exists;
# an operator inspects the daemon, fixes it and deletes the file.
dpj_poison() {
  local reason="$1" pid
  mkdir -p "$BK_DPJ_STATE_DIR"
  printf '%s %s\n' "$(date -u +%FT%TZ)" "$reason" >>"$dpj_poison_file"
  dpj_log "POISONED: $reason (see $dpj_poison_file)"
  if pid="$(dpj_agent_pid)"; then
    # Under --kubernetes-exec the first SIGTERM stops the agent ungracefully:
    # it cancels any running job (here, the one being rejected) and
    # disconnects.
    dpj_log "stopping agent (pid $pid)"
    kill -TERM "$pid" || true
  else
    dpj_log "could not find the buildkite-agent start process to stop it"
  fi
}
