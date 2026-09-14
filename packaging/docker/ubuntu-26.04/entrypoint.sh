#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" != "--buildkite-container-init" ]]; then
  exec /usr/local/bin/buildkite-agent internal-container-launch "$@"
fi
shift

DIR=/docker-entrypoint.d

if [[ -d "$DIR" ]] ; then
  echo "Executing scripts in $DIR"
  /bin/run-parts --exit-on-error "$DIR"
fi

exec ssh-env-config.sh /usr/local/bin/buildkite-agent "$@"
