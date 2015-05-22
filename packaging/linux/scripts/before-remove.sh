#!/bin/sh

set -e

if command -v systemctl > /dev/null; then
  echo "Stopping buildkite-agent systemd service"

  systemctl --no-reload disable buildkite-agent || :
    systemctl stop buildkite-agent || :
elif [ -d /etc/init ]; then
  echo "Stopping buildkite-agent upstart service"

  service buildkite-agent stop || :
elif [ -d /etc/init.d ]; then
  echo "Stopping buildkite-agent init.d script"

  /etc/init.d/buildkite-agent stop || :
fi
