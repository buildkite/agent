FROM alpine:3.12

ARG TARGETARCH
ARG TARGETVARIANT

RUN mkdir /buildkite \
          /buildkite/builds \
          /buildkite/hooks \
          /buildkite/plugins \
          /buildkite/bin

COPY buildkite-agent.cfg /buildkite/
COPY buildkite-agent-linux-${TARGETARCH}${TARGETVARIANT} /buildkite/bin/buildkite-agent

FROM busybox:musl
COPY --from=0 /buildkite /buildkite
VOLUME /buildkite