FROM ubuntu:18.04

ARG TARGETARCH
ARG TARGETVARIANT

ENV DEBIAN_FRONTEND=noninteractive
ENV DOCKER_COMPOSE_VERSION=v2.0.0-rc.3
ENV TINI_VERSION=v0.19.0

RUN apt-get update && apt-get install -y --no-install-recommends \
      apt-transport-https \
      curl \
      ca-certificates \
      bash \
      git \
      gnupg-agent \
      jq \
      openssh-client \
      perl \
      rsync \
      software-properties-common \
    && curl -fsSL https://download.docker.com/linux/ubuntu/gpg | apt-key add - \
    && add-apt-repository \
      "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" \
    && apt-get update \
    && apt-get install -y --no-install-recommends docker-ce-cli \
    && rm -rf /var/lib/apt/lists/*

RUN case "${TARGETARCH}/${TARGETVARIANT}" in \
    "amd64/")   tini_arch=amd64 ;; \
    "arm64/v8") tini_arch=arm64 ;; \
    "arm/v7")   tini_arch=armhf ;; \
    "*") echo "unknown target arch/variant $TARGETARCH/$TARGETVARIANT"; exit 1 ;; \
    esac && \
    curl -Lfs -o /sbin/tini https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini-${tini_arch} \
    && chmod +x /sbin/tini

RUN case "${TARGETARCH}/${TARGETVARIANT}" in \
    "amd64")    docker_arch=amd64 ;; \
    "arm64/v8") docker_arch=arm64 ;; \
    "arm/v7")   docker_arch=armv7 ;; \
    "*")        echo "unknown target arch/variant $TARGETARCH/$TARGETVARIANT"; exit 1 ;; \
    esac && \
    curl -Lfs https://github.com/docker/compose/releases/download/${DOCKER_COMPOSE_VERSION}/docker-compose-linux-${docker_arch} -o /usr/local/bin/docker-compose \
    && chmod +x /usr/local/bin/docker-compose

ENV BUILDKITE_AGENT_CONFIG=/buildkite/buildkite-agent.cfg \
    PATH="/usr/local/bin:${PATH}"

RUN mkdir -p /buildkite/builds /buildkite/hooks /buildkite/plugins \
    && curl -Lfs -o /usr/local/bin/ssh-env-config.sh https://raw.githubusercontent.com/buildkite/docker-ssh-env-config/master/ssh-env-config.sh \
    && chmod +x /usr/local/bin/ssh-env-config.sh

COPY ./buildkite-agent.cfg /buildkite/buildkite-agent.cfg
COPY ./buildkite-agent-linux-${TARGETARCH}${TARGETVARIANT} /usr/local/bin/buildkite-agent
COPY ./entrypoint.sh /usr/local/bin/buildkite-agent-entrypoint

VOLUME /buildkite
ENTRYPOINT ["buildkite-agent-entrypoint"]
CMD ["start"]
