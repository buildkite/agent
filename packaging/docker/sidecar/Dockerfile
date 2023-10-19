FROM public.ecr.aws/docker/library/alpine:3.18.4@sha256:eece025e432126ce23f223450a0326fbebde39cdf496a85d8c016293fc851978

ARG TARGETOS
ARG TARGETARCH

RUN mkdir /buildkite \
  /buildkite/builds \
  /buildkite/hooks \
  /buildkite/plugins \
  /buildkite/bin

COPY buildkite-agent.cfg /buildkite/
COPY buildkite-agent-$TARGETOS-$TARGETARCH /buildkite/bin/buildkite-agent

FROM public.ecr.aws/docker/library/busybox:1-musl@sha256:f553b7484625f0c73bfa3888e013e70e99ec6ae1c424ee0e8a85052bd135a28a
COPY --from=0 /buildkite /buildkite
VOLUME /buildkite
