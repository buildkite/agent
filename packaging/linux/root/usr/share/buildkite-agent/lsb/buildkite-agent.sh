#!/usr/bin/env bash
# shellcheck disable=1090

### BEGIN INIT INFO
# Provides:          buildkite-agent
# Required-Start:    $network $local_fs $remote_fs
# Required-Stop:     $remote_fs
# Should-Start:      $named
# Should-Stop:
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: The Buildkite Build Agent
# Description:       The Buildkite Build Agent
### END INIT INFO

user="buildkite-agent"
cmd="/usr/bin/buildkite-agent start"

name=$(basename "$(readlink -f "$0")")
pid_file="/var/run/${name}.pid"
lock_dir="/var/lock/subsys"
lock_file="${lock_dir}/${name}"
log="/var/log/${name}.log"

[ -r "/etc/default/${name}" ] && . "/etc/default/${name}"
[ -r "/etc/sysconfig/${name}" ] && . "/etc/sysconfig/${name}"

get_pid() {
    cat "$pid_file"
}

is_running() {
    [ -f "$pid_file" ] && ps "$(get_pid)" > /dev/null 2>&1
}

case "$1" in
    start)
    if is_running; then
        echo "Already started"
    else
        echo "Starting $name"
        su --login --shell /bin/sh "$user" --command "exec $cmd" >>"$log" 2>&1 &
        echo $! > "$pid_file"
        if ! is_running; then
            echo "Unable to start, see $log"
            exit 1
        fi
        if [ -d "$lock_dir" ]; then
            touch "$lock_file"
        fi
    fi
    ;;
    stop)
    if is_running; then
        echo -n "Stopping $name.."
        kill "$(get_pid)"
        for _ in {1..10}; do
            if ! is_running; then
                break
            fi

            echo -n "."
            sleep 1
        done
        echo

        if is_running; then
            echo "Not stopped; may still be shutting down or shutdown may have failed"
            exit 1
        else
            echo "Stopped"
            if [ -f "$pid_file" ]; then
                rm "$pid_file"
            fi
            if [ -f "$lock_file" ]; then
                rm -f "$lock_file"
            fi
        fi
    else
        echo "Not running"
    fi
    ;;
    restart)
    $0 stop
    if is_running; then
        echo "Unable to stop, will not attempt to start"
        exit 1
    fi
    $0 start
    ;;
    status)
    if is_running; then
        echo "Running"
    else
        echo "Stopped"
        exit 1
    fi
    ;;
    *)
    echo "Usage: $0 {start|stop|restart|status}"
    exit 1
    ;;
esac

exit 0
