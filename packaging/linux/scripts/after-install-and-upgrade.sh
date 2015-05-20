#!/bin/sh

set -e

# $1 will be the version being upgraded from if this is an upgrade
if [ "$1" = "" ] ; then
  OPERATION="install"
else
  OPERATION="upgrade"
fi

# Create the /etc/buildkite-agent folder if it's not there
if [ ! -d /etc/buildkite-agent ]; then
  mkdir -p /etc/buildkite-agent
fi

# Install the config template if it's not there
if [ ! -f /etc/buildkite-agent/buildkite-agent.cfg ]; then
  cp /usr/share/buildkite-agent/buildkite-agent.cfg /etc/buildkite-agent/buildkite-agent.cfg
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

# Nice welcome message on install
if [ "$OPERATION" = "install" ] ; then
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

  echo "You now need to add your agent token to \"/etc/buildkite-agent/buildkite-agent.cfg\""
  echo "and then you can start your agent by running \"$START_COMMAND\""
fi

# Crude method of causing all the agents to restart
if [ "$OPERATION" = "upgrade" ] ; then
  for KILLPID in `ps ax | grep 'buildkite-agent start' | awk ' { print $1;}'`; do
    kill $KILLPID > /dev/null 2>&1 || true
  done
fi

exit 0
