#!/usr/bin/env bash
set -euo pipefail

DIR=/docker-entrypoint.d

if [[ -d "$DIR" ]] ; then
  echo "Executing scripts in $DIR"
  /bin/run-parts --exit-on-error "$DIR"
fi

exec /usr/bin/tini -- ssh-env-config.sh /usr/local/bin/buildkite-agent "$@"
