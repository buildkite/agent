# syntax=docker/dockerfile:1.4

FROM public.ecr.aws/ubuntu/ubuntu:24.04@sha256:fb95efe0d22be277f10250f15e5172ec0fe22c37eca2ba55e78b526c447eec23

ARG TARGETOS
ARG TARGETARCH

RUN <<BASH
#!/usr/bin/env bash

set -eufo pipefail

export DEBIAN_FRONTEND=noninteractive

# Install main packages
apt-get update
apt-get install -y --no-install-recommends \
  apt-transport-https \
  bash \
  ca-certificates \
  curl \
  git \
  gnupg-agent \
  jq \
  openssh-client \
  perl \
  python3 \
  python3-pip \
  rsync \
  software-properties-common \
  tini

# Install Docker Engine
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo \
    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
    $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
    tee /etc/apt/sources.list.d/docker.list > /dev/null

# We just updated the main sources. This only updates the docker source
apt-get update -o Dir::Etc::sourcelist="sources.list.d/docker.list" \
    -o Dir::Etc::sourceparts="-" \
    -o APT::Get::List-Cleanup="0"
apt-get install -y --no-install-recommends docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

rm -rf /var/lib/apt/lists/*

ln -s /usr/bin/tini /usr/sbin/tini

mkdir -p /buildkite/builds /buildkite/hooks /buildkite/plugins
curl -Lfs -o /usr/local/bin/ssh-env-config.sh https://raw.githubusercontent.com/buildkite/docker-ssh-env-config/master/ssh-env-config.sh
chmod +x /usr/local/bin/ssh-env-config.sh
BASH

ENV BUILDKITE_AGENT_CONFIG=/buildkite/buildkite-agent.cfg \
    PATH="/usr/local/bin:${PATH}"

COPY ./docker-compose /usr/local/bin/docker-compose
COPY ./buildkite-agent.cfg /buildkite/buildkite-agent.cfg
COPY ./buildkite-agent-$TARGETOS-$TARGETARCH /usr/local/bin/buildkite-agent
COPY ./entrypoint.sh /usr/local/bin/buildkite-agent-entrypoint

VOLUME /buildkite
ENTRYPOINT ["buildkite-agent-entrypoint"]
CMD ["start"]
