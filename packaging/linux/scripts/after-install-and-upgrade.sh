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
    # If the system has the old .env file, install the old upstart script, and
    # let them know they should upgrade. Because the upstart script is no
    # longer considered a `config` file in the debian package, when you upgrade
    # the agent, it rm's it from /etc/init, boo. So we need to put it back.
    if [ -f /etc/buildkite-agent/buildkite-agent.env ]; then
      echo "======================= IMPORTANT UPGRADE NOTICE =========================="
      echo ""
      echo "Hey!"
      echo ""
      echo "Sorry to be a pain, but we've deprecated use of the"
      echo "/etc/buildkite-agent/buildkite-agent.env ENV file as a way of configuring the agent."
      echo "It's had some issues and the approach wasn't very cross platform."
      echo ""
      echo "We've switched to using a proper config file that you can find here:"
      echo ""
      echo "/etc/buildkite-agent/buildkite-agent.cfg"
      echo ""
      echo "Everything should continue to work as is (we'll still use the .env file for now)."
      echo "To upgrade, all you need to do is edit the new config file and copy across the settings"
      echo "your .env file, then run:"
      echo ""
      echo "sudo service buildkite-agent stop"
      echo "sudo rm /etc/buildkite-agent/buildkite-agent.env"
      echo "sudo cp /usr/share/buildkite-agent/upstart/buildkite-agent.conf /etc/init/buildkite-agent.conf"
      echo "sudo service buildkite-agent start"
      echo ""
      echo "Then next time you upgrade, you won't see this annoying message :)"
      echo ""
      echo "If you have any questions, feel free to email me at: keith@buildkite.com"
      echo ""
      echo "~ Keith"
      echo ""
      echo "=========================================================================="
      echo ""

      cp /usr/share/buildkite-agent/upstart/buildkite-agent-using-env.conf /etc/init/buildkite-agent.conf
    else
      cp /usr/share/buildkite-agent/upstart/buildkite-agent.conf /etc/init/buildkite-agent.conf
    fi
  fi

  START_COMMAND="sudo service buildkite-agent start"
elif [ -d /etc/init.d ]; then
  if [ ! -f /etc/init.d/buildkite-agent ]; then
    cp /usr/share/buildkite-agent/lsb/buildkite-agent.conf /etc/init.d/buildkite-agent
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
