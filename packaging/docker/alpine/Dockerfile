FROM public.ecr.aws/docker/library/alpine:3.20.1@sha256:b89d9c93e9ed3597455c90a0b88a8bbb5cb7188438f70953fede212a0c4394e0

ARG TARGETOS
ARG TARGETARCH

RUN apk add --no-cache \
    bash \
    curl \
    docker-cli \
    docker-cli-compose \
    docker-cli-buildx \
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

COPY docker-compose /usr/local/bin/docker-compose

ENV BUILDKITE_AGENT_CONFIG=/buildkite/buildkite-agent.cfg

RUN mkdir -p /buildkite/builds /buildkite/hooks /buildkite/plugins \
    && curl -Lfs -o /usr/local/bin/ssh-env-config.sh https://raw.githubusercontent.com/buildkite/docker-ssh-env-config/master/ssh-env-config.sh \
    && chmod +x /usr/local/bin/ssh-env-config.sh

COPY ./buildkite-agent.cfg /buildkite/buildkite-agent.cfg
COPY ./buildkite-agent-$TARGETOS-$TARGETARCH /usr/local/bin/buildkite-agent
COPY ./entrypoint.sh /usr/local/bin/buildkite-agent-entrypoint

VOLUME /buildkite
ENTRYPOINT ["buildkite-agent-entrypoint"]
CMD ["start"]
