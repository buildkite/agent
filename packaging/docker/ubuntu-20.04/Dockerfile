# syntax=docker/dockerfile:1.4

FROM public.ecr.aws/buildkite/agent-base:ubuntu-focal@sha256:ef7a96525cff860c9a6919ef43f8c95f75269924d07633f09fb2a415427c89c4
ARG TARGETOS
ARG TARGETARCH

ENV BUILDKITE_AGENT_CONFIG=/buildkite/buildkite-agent.cfg \
    PATH="/usr/local/bin:${PATH}"

RUN mkdir -p /buildkite/builds /buildkite/hooks /buildkite/plugins \
    && curl -Lfs -o /usr/local/bin/ssh-env-config.sh https://raw.githubusercontent.com/buildkite/docker-ssh-env-config/master/ssh-env-config.sh \
    && chmod +x /usr/local/bin/ssh-env-config.sh

COPY ./buildkite-agent.cfg /buildkite/buildkite-agent.cfg
COPY ./buildkite-agent-$TARGETOS-$TARGETARCH /usr/local/bin/buildkite-agent
COPY ./entrypoint.sh /usr/local/bin/buildkite-agent-entrypoint

VOLUME /buildkite
ENTRYPOINT ["buildkite-agent-entrypoint"]
CMD ["start"]
