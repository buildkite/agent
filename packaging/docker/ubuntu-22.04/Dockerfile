# syntax=docker/dockerfile:1.4

FROM public.ecr.aws/ubuntu/ubuntu:22.04@sha256:606a9e0ca22a44f8956dcb463ab4daba5d50c100ea8075d3c24a2bddd161d320

ARG TARGETOS
ARG TARGETARCH

RUN <<BASH
#!/usr/bin/env bash

set -eufo pipefail

export DEBIAN_FRONTEND=noninteractive

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

curl -fsSL https://download.docker.com/linux/ubuntu/gpg | apt-key add -
add-apt-repository "deb [arch=$TARGETARCH] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable"
apt-get update
apt-get install -y --no-install-recommends docker-ce-cli docker-compose-plugin docker-buildx-plugin
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
