#!/bin/sh

# This is a shim to make the upgrade process from v2 -> v3 smoother as
# v2 configurations will still have bootstrap-path=/usr/share/buildkite-agent/bootstrap.sh
# which will cause all sorts of wierd behaviour

echo "+++ :warning: Your agent configuration contains an outdated bootstrap-path reference. It can be safely removed."
exec buildkite-agent bootstrap "$@"
