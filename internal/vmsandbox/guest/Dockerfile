# Guest root filesystem for the VM sandbox. This is not a container image
# that runs anywhere: build-rootfs.sh exports it to an ext4 image that boots
# under Firecracker with init.sh as PID 1.
#
# Everything the job might need must be here. There is no host filesystem in
# the guest.
FROM ubuntu:24.04

ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        openssh-client \
        iproute2 \
        iptables \
        procps \
        docker.io \
        docker-compose-v2 \
    && rm -rf /var/lib/apt/lists/*

# Layout expected by the host runner (see ../guestenv.go). Hooks are baked
# into the image: the host's hooks directory is not available in the guest.
RUN mkdir -p \
        /var/lib/buildkite-agent/builds \
        /var/lib/buildkite-agent/plugins \
        /var/lib/buildkite-agent/sockets \
        /var/lib/buildkite-agent/job-context \
        /etc/buildkite-agent/hooks \
        /root/.ssh

# The agent binary, built for linux/arm64 by build-rootfs.sh. Only the
# `bootstrap` and `vm-guest-bootstrap` subcommands are used in the guest.
COPY build/buildkite-agent /usr/local/bin/buildkite-agent
COPY init.sh /sbin/init
RUN chmod 0755 /usr/local/bin/buildkite-agent /sbin/init

# Docker's default bridge in the guest. Keep it off the host tap's range.
RUN mkdir -p /etc/docker && printf '{"bip":"10.88.0.1/24"}\n' > /etc/docker/daemon.json
