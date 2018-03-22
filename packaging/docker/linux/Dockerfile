FROM alpine:3.6

RUN apk add --no-cache \
    tini \
    bash \
    git \
    perl \
    rsync \
    openssh-client \
    curl \
    docker \
    jq \
    su-exec \
    py-pip \
    libc6-compat \
    run-parts \
    && \
    pip install docker-compose

ENV BUILDKITE_BUILD_PATH=/buildkite/builds \
    BUILDKITE_HOOKS_PATH=/buildkite/hooks \
    BUILDKITE_PLUGINS_PATH=/buildkite/plugins

RUN curl -Lfs -o /usr/local/bin/buildkite-agent https://download.buildkite.com/agent/experimental/latest/buildkite-agent-linux-amd64 \
    && chmod +x /usr/local/bin/buildkite-agent \
    && mkdir -p /buildkite/builds /buildkite/hooks /buildkite/plugins \
    && curl -Lfs -o /usr/local/bin/ssh-env-config.sh https://raw.githubusercontent.com/buildkite/docker-ssh-env-config/master/ssh-env-config.sh \
    && chmod +x /usr/local/bin/ssh-env-config.sh

COPY ./entrypoint.sh /usr/local/bin/buildkite-agent-entrypoint

VOLUME /buildkite
ENTRYPOINT ["buildkite-agent-entrypoint"]
CMD ["start"]
