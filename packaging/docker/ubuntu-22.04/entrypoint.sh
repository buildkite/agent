#!/usr/bin/env bash
set -euo pipefail

case "${BUILDKITE_AGENT_TOKEN:-}" in
  ''|fd://*|file://*) ;;
  *)
    if ! (export LC_ALL=C; (( ${#BUILDKITE_AGENT_TOKEN} <= 4096 ))); then
      printf '%s\n' 'BUILDKITE_AGENT_TOKEN exceeds the 4096-byte limit' >&2
      exit 1
    fi
    exec {token_fd}< <(printf '%s' "$BUILDKITE_AGENT_TOKEN")
    wait "$!"
    export BUILDKITE_AGENT_TOKEN="fd://$token_fd" BUILDKITE_AGENT_CONTAINER_TOKEN_FD="$token_fd"
    exec /bin/bash "$0" "$@"
    ;;
esac

if [[ "${1:-}" == --buildkite-container-argv-ready ]]; then
  shift
else
  exec /usr/local/bin/buildkite-container-launch "$@"
fi

DIR=/docker-entrypoint.d

if [[ -d "$DIR" ]] ; then
  echo "Executing scripts in $DIR"
  env -u BUILDKITE_AGENT_CONTAINER_TOKEN_FD /bin/run-parts --exit-on-error "$DIR"
fi

exec /usr/bin/tini -- ssh-env-config.sh /usr/local/bin/buildkite-agent "$@"
