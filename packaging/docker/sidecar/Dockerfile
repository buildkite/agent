FROM public.ecr.aws/docker/library/alpine:3.20.3@sha256:beefdbd8a1da6d2915566fde36db9db0b524eb737fc57cd1367effd16dc0d06d

ARG TARGETOS
ARG TARGETARCH

RUN mkdir /buildkite \
  /buildkite/builds \
  /buildkite/hooks \
  /buildkite/plugins \
  /buildkite/bin

COPY buildkite-agent.cfg /buildkite/
COPY buildkite-agent-$TARGETOS-$TARGETARCH /buildkite/bin/buildkite-agent

FROM public.ecr.aws/docker/library/busybox:1-musl@sha256:b8741adb90b0f4525bfd7cb9f08c8bdd86ac36c73dd71daaa2d34f968e9f78c5
COPY --from=0 /buildkite /buildkite
VOLUME /buildkite
