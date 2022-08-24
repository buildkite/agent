FROM alpine:3.16.1

RUN mkdir /buildkite \
  /buildkite/builds \
  /buildkite/hooks \
  /buildkite/plugins \
  /buildkite/bin

COPY buildkite-agent.cfg /buildkite/
COPY buildkite-agent /buildkite/bin/

FROM busybox:musl
COPY --from=0 /buildkite /buildkite
VOLUME /buildkite
