#!/bin/sh

set -e

for KILLPID in `ps ax | grep 'buildkite-agent start' | awk ' { print $1;}'`; do
  kill $KILLPID > /dev/null 2>&1 || true
done

# Return true so if nothing was killed, we don't error the upgrade
true
