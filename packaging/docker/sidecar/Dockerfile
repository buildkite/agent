FROM public.ecr.aws/docker/library/alpine:3.20.0@sha256:77726ef6b57ddf65bb551896826ec38bc3e53f75cdde31354fbffb4f25238ebd

ARG TARGETOS
ARG TARGETARCH

RUN mkdir /buildkite \
  /buildkite/builds \
  /buildkite/hooks \
  /buildkite/plugins \
  /buildkite/bin

COPY buildkite-agent.cfg /buildkite/
COPY buildkite-agent-$TARGETOS-$TARGETARCH /buildkite/bin/buildkite-agent

FROM public.ecr.aws/docker/library/busybox:1-musl@sha256:ede585a46e84850ffa6e3331c53ac571fe71d9b1ba9299f7925b052985db3e7f
COPY --from=0 /buildkite /buildkite
VOLUME /buildkite
