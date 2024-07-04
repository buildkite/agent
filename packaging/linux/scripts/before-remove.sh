#!/usr/bin/env bash

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

if command -v systemctl > /dev/null; then
  echo "Stopping buildkite-agent systemd service"

  systemctl --no-reload disable buildkite-agent || :
  systemctl stop buildkite-agent || :

  systemctl --no-reload disable "buildkite-agent@" || :
  systemctl stop "buildkite-agent@*" || :
elif [ $BK_UPSTART_EXISTS -eq 0 ] && [ $BK_UPSTART_TOO_OLD -eq 0 ]; then
  echo "Stopping buildkite-agent upstart service"

  service buildkite-agent stop || :
elif [ -d /etc/init.d ]; then
  echo "Stopping buildkite-agent init.d script"

  /etc/init.d/buildkite-agent stop || :
  command -v chkconfig > /dev/null && chkconfig --del buildkite-agent || :
fi
