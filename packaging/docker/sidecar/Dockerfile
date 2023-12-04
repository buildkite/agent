FROM public.ecr.aws/docker/library/alpine:3.18.5@sha256:34871e7290500828b39e22294660bee86d966bc0017544e848dd9a255cdf59e0

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
