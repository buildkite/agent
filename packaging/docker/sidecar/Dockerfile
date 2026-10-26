FROM public.ecr.aws/docker/library/alpine:3.22.0@sha256:8a1f59ffb675680d47db6337b49d22281a139e9d709335b492be023728e11715

ARG TARGETOS
ARG TARGETARCH

RUN mkdir /buildkite \
  /buildkite/builds \
  /buildkite/hooks \
  /buildkite/plugins \
  /buildkite/bin

COPY buildkite-agent.cfg /buildkite/
COPY buildkite-agent-$TARGETOS-$TARGETARCH /buildkite/bin/buildkite-agent

FROM public.ecr.aws/docker/library/busybox:1-musl@sha256:ea2b9914a16a4ac1981994af97b318f7c7d4db76b580c56177f08bf76f4a0be8
COPY --from=0 /buildkite /buildkite
VOLUME /buildkite
