# Remove the system service we installed
if command -v systemctl > /dev/null; then
  rm -f /lib/systemd/system/buildkite-agent.service
elif [ -d /etc/init ]; then
  rm -f /etc/init/buildkite-agent.conf
elif [ -d /etc/init.d ]; then
  rm -f /etc/init.d/buildkite-agent
fi
