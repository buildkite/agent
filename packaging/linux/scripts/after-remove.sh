#!/usr/bin/env bash

# Remove the system service we installed
if command -v systemctl > /dev/null; then
  rm -f /lib/systemd/system/buildkite-agent.service
  rm -f /lib/systemd/system/buildkite-agent@.service
fi
if [ -f /etc/init/buildkite-agent.conf ]; then
  rm -f /etc/init/buildkite-agent.conf
fi
if [ -f /etc/init.d/buildkite-agent ]; then
  rm -f /etc/init.d/buildkite-agent
fi
