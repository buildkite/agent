#!/bin/sh

set -e

cat <<"TXT"
 _           _ _     _ _    _ _                                _
| |         (_) |   | | |  (_) |                              | |
| |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_
| '_ \| | | | | |/ _` | |/ / | __/ _ \  / _` |/ _` |/ _ \ '_ \| __|
| |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_
|_.__/ \__,_|_|_|\__,_|_|\_\_|\__\___|  \__,_|\__, |\___|_| |_|\__|
                                               __/ |
                                              |___/

TXT

# Create the /etc/buildkite-agent folder if it's not there
if [ ! -d /etc/buildkite-agent ]; then
  mkdir -p /etc/buildkite-agent
fi

# Install the config template if it's not there
if [ ! -f /etc/buildkite-agent/buildkite-agent.cfg ]; then
  cp /usr/share/buildkite-agent/buildkite-agent.cfg /etc/buildkite-agent/buildkite-agent.cfg
fi

# Install the bootstrap.sh file if it doesn't exist
if [ ! -f /etc/buildkite-agent/bootstrap.sh ]; then
  cp /usr/share/buildkite-agent/bootstrap.sh /etc/buildkite-agent/bootstrap.sh
fi

# Copy the hooks if they aren't there
if [ ! -d /etc/buildkite-agent/hooks ]; then
  cp -r /usr/share/buildkite-agent/hooks /etc/buildkite-agent
fi

# Install the relevant system process
if command -v systemctl > /dev/null; then
  if [ ! -f /lib/systemd/system/buildkite-agent.service ]; then
    cp /usr/share/buildkite-agent/systemd/buildkite-agent.service /lib/systemd/system/buildkite-agent.service
  fi

  START_COMMAND="sudo systemctl enable buildkite-agent && sudo systemctl start buildkite-agent"
elif [ -d /etc/init ]; then
  if [ ! -f /etc/init/buildkite-agent.conf ]; then
    cp -r /usr/share/buildkite-agent/upstart/buildkite-agent.conf /etc/init/buildkite-agent.conf
  fi

  START_COMMAND="sudo service buildkite-agent start"
elif [ -d /etc/init.d ]; then
  if [ ! -f /etc/init.d/buildkite-agent ]; then
    cp -r /usr/share/buildkite-agent/lsb/buildkite-agent.conf /etc/init.d/buildkite-agent
  fi

  START_COMMAND="sudo /etc/init.d/buildkite-agent start"
else
  # If all the others fails, warn them and just let them run it the old
  # fasioned way.
  echo "============================== WARNING ==================================="
  echo ""
  echo "The Buildkite Agent could not find a suitable system service to install."
  echo "Please open an issue at https://github.com/buildkite/agent and let us know"
  echo ""
  echo "=========================================================================="
  echo ""

  START_COMMAND="sudo /usr/bin/buildkite-agent start"
fi

echo "You now need to add your agent token to \"/etc/buildkite-agent/buildkite-agent.cfg\""
echo "and then you can start your agent by running \"$START_COMMAND\""

exit 0
