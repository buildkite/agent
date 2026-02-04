#!/usr/bin/env bash

set -o errexit
set -o nounset

if [[ ${#} -ne 1 ]]
then
  echo "Usage: ${0} upstart-conf-file" >&2
  exit 1
fi
config=${1} && shift

dbus_pid_file=$(/bin/mktemp)
exec 4<> ${dbus_pid_file}

dbus_add_file=$(/bin/mktemp)
exec 6<> ${dbus_add_file}

/bin/dbus-daemon --fork --print-pid 4 --print-address 6 --session

function clean {
  kill $(cat ${dbus_pid_file})
  rm -f ${dbus_pid_file} ${dbus_add_file}
  exit 1
}
trap "{ clean; }" EXIT

export DBUS_SESSION_BUS_ADDRESS=$(cat ${dbus_add_file})

/bin/init-checkconf ${config}
