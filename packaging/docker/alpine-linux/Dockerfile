FROM alpine:3.12

ARG TARGETARCH
ARG TARGETVARIANT

RUN apk add --no-cache \
      bash \
      curl \
      docker-cli \
      docker-compose \
      git \
      jq \
      libc6-compat \
      openssh-client \
      perl \
      py-pip \
      rsync \
      run-parts \
      su-exec \
      tini \
      tzdata

ENV BUILDKITE_AGENT_CONFIG=/buildkite/buildkite-agent.cfg

RUN mkdir -p /buildkite/builds /buildkite/hooks /buildkite/plugins \
    && curl -Lfs -o /usr/local/bin/ssh-env-config.sh https://raw.githubusercontent.com/buildkite/docker-ssh-env-config/master/ssh-env-config.sh \
    && chmod +x /usr/local/bin/ssh-env-config.sh

COPY ./buildkite-agent.cfg /buildkite/buildkite-agent.cfg
COPY ./buildkite-agent-linux-${TARGETARCH}${TARGETVARIANT} /usr/local/bin/buildkite-agent
COPY ./entrypoint.sh /usr/local/bin/buildkite-agent-entrypoint

VOLUME /buildkite
ENTRYPOINT ["buildkite-agent-entrypoint"]
CMD ["start"]
