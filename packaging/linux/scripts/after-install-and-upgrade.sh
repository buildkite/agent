#!/usr/bin/env bash

# $1 will be the version being upgraded from if this is an upgrade
if [ "$1" = "" ] ; then
  OPERATION="install"
else
  OPERATION="upgrade"
fi

# Find out whether or not the buildkite-agent user exists
if [ -z "$(getent passwd buildkite-agent)" ]; then
  BK_USER_EXISTS="false"
else
  BK_USER_EXISTS="true"
fi

# Add the buildkite user if it doesn't exist on installation
if [ "$OPERATION" = "install" ] ; then
  if [ "$BK_USER_EXISTS" = "false" ]; then
    # Create the buildkite system user and set its home to /var/lib/buildkite
    useradd --system --no-create-home -d /var/lib/buildkite-agent buildkite-agent

    # The user exists now!
    BK_USER_EXISTS=true
  fi

  # We create its home folder in a seperate command so it doesn't blow up if
  # the folder already exists
  mkdir -p /var/lib/buildkite-agent
fi

# Create the /etc/buildkite-agent folder if it's not there
if [ ! -d /etc/buildkite-agent ]; then
  mkdir -p /etc/buildkite-agent
fi

# Install the config template if it's not there
if [ ! -f /etc/buildkite-agent/buildkite-agent.cfg ]; then
  cp /usr/share/buildkite-agent/buildkite-agent.cfg /etc/buildkite-agent/buildkite-agent.cfg

  # Set the default permission to 0600 so only the owning user can read/write to the file
  chmod 0600 /etc/buildkite-agent/buildkite-agent.cfg
fi

# Copy the hooks if they aren't there
if [ ! -d /etc/buildkite-agent/hooks ]; then
  cp -r /usr/share/buildkite-agent/hooks /etc/buildkite-agent
fi

# Check if systemd exists
command -v systemctl > /dev/null
BK_SYSTEMD_EXISTS=$?

# Try to install a systemd unit
if [ $BK_SYSTEMD_EXISTS -eq 0 ]; then
  cp /usr/share/buildkite-agent/systemd/buildkite-agent.service /lib/systemd/system/buildkite-agent.service
  cp /usr/share/buildkite-agent/systemd/buildkite-agent@.service /lib/systemd/system/buildkite-agent@.service

  START_COMMAND="sudo systemctl enable buildkite-agent && sudo systemctl start buildkite-agent"
elif [ -d /etc/init.d ]; then
  # Fall back to system v init script
  if [ ! -f /etc/init.d/buildkite-agent ]; then
    cp /usr/share/buildkite-agent/lsb/buildkite-agent.sh /etc/init.d/buildkite-agent
    command -v chkconfig > /dev/null && chkconfig --add buildkite-agent
  fi

  START_COMMAND="sudo /etc/init.d/buildkite-agent start"
else
  # If all the others fails, warn them and just let them run it the old
  # fashioned way.
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

# Restart on upgrade, if we can
if [ "$OPERATION" = "upgrade" ] ; then
  # Try restarting with systemd, if the unit is active.
  #
  # Why two patterns?
  #
  # The second pattern is for instantiations of the template.
  # Trying to find one pattern that covers such instantiations and the base
  # unit is possible, it is `buildkite-agent*`. However that would also cover
  # other service names such as `buildkite-agentless`.
  # It's safer to use two, more restrictive patterns.
  #
  # Does using two patterns in the same invocation work?
  #
  # `is-active` is used to check if any of the base or template instance
  # services are running.
  # `try-restart` will then restart any of them if they are running.
  # See man systemctl for more details
  #
  # Thus if any of them are running, they will be restarted respectively.
  if [ $BK_SYSTEMD_EXISTS -eq 0 ] && systemctl is-active buildkite-agent 'buildkite-agent@*' --quiet; then
    systemctl try-restart buildkite-agent 'buildkite-agent@*'
  elif [ -x /etc/init.d/buildkite-agent ] && /etc/init.d/buildkite-agent status > /dev/null 2>&1; then
    # Fall back to systemv, if the process is running
    /etc/init.d/buildkite-agent restart
  else
    # Kill agents and hope they restart, looking for command line containing
    # "buildkite-agent v1.2.3.4" or "buildkite-agent start"
    pkill -f 'buildkite-agent (v|start)' > /dev/null 2>&1 || true
  fi
fi

# Make sure all the folders created are owned by the buildkite-agent user #
# on install
if [ "$OPERATION" = "install" ] ; then
  if [ "$BK_USER_EXISTS" = "true" ]; then
    # Make sure /etc/buildkite-agent is owned by the user
    chown -R buildkite-agent:buildkite-agent /etc/buildkite-agent

    # Only chown the /var/lib/buildkite-agent folder if it was created
    if [ -d /var/lib/buildkite-agent ]; then
      chown -R buildkite-agent:buildkite-agent /var/lib/buildkite-agent
    fi
  fi
fi

exit 0
