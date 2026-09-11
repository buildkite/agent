#!/bin/bash
# PID 1 inside the sandbox guest.
#
# Bring up just enough of a system for `buildkite-agent bootstrap` and
# dockerd, hand over to `buildkite-agent vm-guest-bootstrap` (which talks to
# the host over vsock, runs the job, and powers the machine off), and make
# sure the machine powers off even if that fails.
#
# The kernel has already configured eth0 from the `ip=` boot argument.

set -u

# Log lines carry the guest uptime so boot phases can be timed from the
# console log.
log() { echo "[init $(cut -d' ' -f1 /proc/uptime 2>/dev/null || echo '?')s] $*"; }

poweroff_now() {
    log "powering off"
    sync
    # MAGIC_SYSRQ is compiled into the pinned kernel. If this somehow fails
    # and init exits, the kernel panics, and `panic=1` on the command line
    # then restarts the machine, which with `reboot=k` makes Firecracker
    # exit. Either way the host sees the VMM go away.
    echo o > /proc/sysrq-trigger
    sleep 5
    exit 1
}
trap poweroff_now EXIT

mount -t proc proc /proc
log "init starting"
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev 2>/dev/null || true
mkdir -p /dev/pts /dev/shm /run /tmp
mount -t devpts devpts /dev/pts -o gid=5,mode=620,ptmxmode=666
mount -t tmpfs tmpfs /dev/shm
mount -t tmpfs tmpfs /run -o mode=755
mount -t tmpfs tmpfs /tmp
mount -t cgroup2 cgroup2 /sys/fs/cgroup 2>/dev/null || log "warning: cgroup2 mount failed"
# devtmpfs hides the image's /dev, and the kernel doesn't create these
# symlinks (udev normally does). Without them bash process substitution
# (<(...) opens /dev/fd/N) and anything writing to /dev/stdout breaks.
ln -sfn /proc/self/fd /dev/fd
ln -sfn /proc/self/fd/0 /dev/stdin
ln -sfn /proc/self/fd/1 /dev/stdout
ln -sfn /proc/self/fd/2 /dev/stderr
# Remount root read-write in case the kernel mounted it ro.
mount -o remount,rw / 2>/dev/null || true

hostname bko-sandbox
echo bko-sandbox > /etc/hostname
printf '127.0.0.1 localhost\n127.0.1.1 bko-sandbox\n' > /etc/hosts

ip link set lo up

# DNS servers come from the host via the kernel command line.
dns=$(tr ' ' '\n' < /proc/cmdline | sed -n 's/^bko_dns=//p' | tr ',' '\n')
: > /etc/resolv.conf
for ns in $dns; do
    echo "nameserver $ns" >> /etc/resolv.conf
done

export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export HOME=/root

# Docker daemon. Its data-root lives on the (per-job, disposable) root
# disk, so every job starts with no images, containers or volumes.
mkdir -p /var/log
log "starting dockerd"
dockerd > /var/log/dockerd.log 2>&1 &
for _ in $(seq 1 60); do
    if docker info > /dev/null 2>&1; then
        log "dockerd ready"
        break
    fi
    sleep 0.5
done
if ! docker info > /dev/null 2>&1; then
    log "warning: dockerd did not become ready; tail of /var/log/dockerd.log:"
    tail -n 20 /var/log/dockerd.log
fi

log "starting vm-guest-bootstrap"
buildkite-agent vm-guest-bootstrap
log "vm-guest-bootstrap exited with status $?"
# vm-guest-bootstrap powers off on its own; if we get here it couldn't, so
# the EXIT trap does it instead.
