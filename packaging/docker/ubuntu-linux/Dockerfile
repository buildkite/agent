FROM ubuntu:18.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      curl \
      ca-certificates \
      bash \
      git \
      perl \
      rsync \
      openssh-client \
      curl \
      docker.io \
      jq \
    && rm -rf /var/lib/apt/lists/*

RUN curl -Lfs -o /sbin/tini  https://github.com/krallin/tini/releases/download/v0.18.0/tini \
    && chmod +x /sbin/tini \
    && curl -Lfs https://github.com/docker/compose/releases/download/1.24.0/docker-compose-Linux-x86_64 -o /usr/local/bin/docker-compose \
    && chmod +x /usr/local/bin/docker-compose

ENV BUILDKITE_AGENT_CONFIG=/buildkite/buildkite-agent.cfg \
    PATH="/usr/local/bin:${PATH}"

RUN mkdir -p /buildkite/builds /buildkite/hooks /buildkite/plugins \
    && curl -Lfs -o /usr/local/bin/ssh-env-config.sh https://raw.githubusercontent.com/buildkite/docker-ssh-env-config/master/ssh-env-config.sh \
    && chmod +x /usr/local/bin/ssh-env-config.sh

COPY ./buildkite-agent.cfg /buildkite/buildkite-agent.cfg
COPY ./buildkite-agent /usr/local/bin/buildkite-agent
COPY ./entrypoint.sh /usr/local/bin/buildkite-agent-entrypoint

VOLUME /buildkite
ENTRYPOINT ["buildkite-agent-entrypoint"]
CMD ["start"]
