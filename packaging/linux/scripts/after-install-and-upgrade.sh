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

# Check if the system is Ubuntu 14.10. Systemd is broken on this release, so if
# even if systemd exists on that system, skip using it.
command -v lsb_release > /dev/null && lsb_release -d | grep -q "Ubuntu 14.10"
BK_IS_UBUNTU_14_10=$?

# Check if systemd exists
command -v systemctl > /dev/null
BK_SYSTEMD_EXISTS=$?

# Check if upstart exists
command -v initctl > /dev/null
BK_UPSTART_EXISTS=$?

# Check if upstart is version 0.6.5 as seen on Amazon linux, RHEL6 & CentOS-6
BK_UPSTART_TOO_OLD=0
if [ $BK_UPSTART_EXISTS -eq 0 ]; then
  BK_UPSTART_VERSION="$(initctl --version | awk 'BEGIN{FS="[ ()]"} NR==1{print $4}')"
  if [ "$BK_UPSTART_VERSION" = "0.6.5" ]; then
    BK_UPSTART_TOO_OLD=1
  fi
fi

# Install the relevant system process
if [ $BK_SYSTEMD_EXISTS -eq 0 ] && [ $BK_IS_UBUNTU_14_10 -eq 1 ]; then
  cp /usr/share/buildkite-agent/systemd/buildkite-agent.service /lib/systemd/system/buildkite-agent.service
  cp /usr/share/buildkite-agent/systemd/buildkite-agent@.service /lib/systemd/system/buildkite-agent@.service

  START_COMMAND="sudo systemctl enable buildkite-agent && sudo systemctl start buildkite-agent"
elif [ $BK_UPSTART_EXISTS -eq 0 ] && [ $BK_UPSTART_TOO_OLD -eq 0 ]; then
  if [ ! -f /etc/init/buildkite-agent.conf ]; then
    cp /usr/share/buildkite-agent/upstart/buildkite-agent.conf /etc/init/buildkite-agent.conf
  fi

  START_COMMAND="sudo service buildkite-agent start"
elif [ -d /etc/init.d ]; then
  if [ ! -f /etc/init.d/buildkite-agent ]; then
    cp /usr/share/buildkite-agent/lsb/buildkite-agent.sh /etc/init.d/buildkite-agent
    command -v chkconfig > /dev/null && chkconfig --add buildkite-agent
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
  # Restart agents that have a process name of "buildkite-agent v1.2.3.4"
  for KILLPID in `ps ax | grep 'buildkite-agent v' | awk ' { print $1;}'`; do
    kill $KILLPID > /dev/null 2>&1 || true
  done

  # Restart agents that have a process name of "buildkite-agent start"
  for KILLPID in `ps ax | grep 'buildkite-agent start' | awk ' { print $1;}'`; do
    kill $KILLPID > /dev/null 2>&1 || true
  done
fi

# Make sure all the the folders created are owned by the buildkite-agent user #
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
