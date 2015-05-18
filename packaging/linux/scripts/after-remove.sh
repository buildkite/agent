#!/bin/sh

set -e

# Remove the system service we installed
if command -v systemctl > /dev/null; then
  rm -f /lib/systemd/system/buildkite-agent.service
elif [ -d /etc/init ]; then
  rm -f /etc/init/buildkite-agent.conf
elif [ -d /etc/init.d ]; then
  rm -f /etc/init.d/buildkite-agent
fi

echo "WHAT IS $1"

# If we've been asked to purge the install, remove all # traces of the
# buildkite-agent
# See: https://www.debian.org/doc/debian-policy/ch-maintainerscripts.html
if [ "$1" = "purge" ] ; then
  echo "Purging buildkite-agent configuration"

  rm -f /etc/buildkite-agent
fi
