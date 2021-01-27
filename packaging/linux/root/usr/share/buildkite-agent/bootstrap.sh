#!/bin/sh

# This is a shim to make the upgrade process from v2 -> v3 smoother as
# v2 configurations will still have bootstrap-path=/usr/share/buildkite-agent/bootstrap.sh
# which will cause all sorts of wierd behaviour

echo "+++ :warning: Your buildkite-agent.cfg file contains a deprecated bootstrap.sh"
echo "As part of the upgrade from Agent v2 to v3, a bootstrap-script compatibility shim was added to your buildkite-agent.cfg."
echo ""
echo "To silence this warning, you can comment out the bootstrap-script line in ${BUILDKITE_CONFIG_PATH:-buildkite-agent.cfg}."
echo ""
echo "For more information, see our Buildkite Agent 3.0 upgrade guide:"
echo "https://buildkite.com/docs/agent/upgrading-to-v3"

exec buildkite-agent bootstrap "$@"
