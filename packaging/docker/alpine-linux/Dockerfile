FROM alpine:3.8

RUN apk add --no-cache \
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
      tini \
      tzdata \
    && \
    pip install --upgrade pip && \
    pip install --quiet docker-compose~=1.23.0

ENV BUILDKITE_AGENT_CONFIG=/buildkite/buildkite-agent.cfg

RUN mkdir -p /buildkite/builds /buildkite/hooks /buildkite/plugins \
    && curl -Lfs -o /usr/local/bin/ssh-env-config.sh https://raw.githubusercontent.com/buildkite/docker-ssh-env-config/master/ssh-env-config.sh \
    && chmod +x /usr/local/bin/ssh-env-config.sh

COPY ./buildkite-agent.cfg /buildkite/buildkite-agent.cfg
COPY ./buildkite-agent /usr/local/bin/buildkite-agent
COPY ./entrypoint.sh /usr/local/bin/buildkite-agent-entrypoint

VOLUME /buildkite
ENTRYPOINT ["buildkite-agent-entrypoint"]
CMD ["start"]
