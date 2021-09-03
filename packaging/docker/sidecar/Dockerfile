FROM alpine:3.12

ARG TARGETARCH

RUN mkdir /buildkite \
          /buildkite/builds \
          /buildkite/hooks \
          /buildkite/plugins \
          /buildkite/bin

COPY buildkite-agent.cfg /buildkite/
COPY buildkite-agent-linux-${TARGETARCH} /buildkite/bin/

FROM busybox:musl
COPY --from=0 /buildkite /buildkite
VOLUME /buildkite