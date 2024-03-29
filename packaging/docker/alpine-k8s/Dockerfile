# syntax=docker/dockerfile:1.4

FROM public.ecr.aws/docker/library/alpine:3.19.1@sha256:c5b1261d6d3e43071626931fc004f70149baeba2c8ec672bd4f27761f8e1ad6b AS base

RUN apk update && apk add --no-cache \
    bash \
    curl \
    docker-cli \
    docker-cli-buildx \
    docker-cli-compose \
    git \
    jq \
    libc6-compat \
    openssh-client \
    perl \
    py-pip \
    rsync \
    run-parts \
    su-exec \
    tini \
    tzdata

COPY docker-compose /usr/local/bin/docker-compose

FROM public.ecr.aws/docker/library/alpine:3.19.1@sha256:c5b1261d6d3e43071626931fc004f70149baeba2c8ec672bd4f27761f8e1ad6b AS kubectl-downloader
ARG TARGETOS
ARG TARGETARCH

RUN <<EOF
set -eu

VERSION=$(wget -qO- https://dl.k8s.io/release/stable.txt)
wget -qO kubectl \
    "https://storage.googleapis.com/kubernetes-release/release/$VERSION/bin/linux/$TARGETARCH/kubectl"
chmod +x kubectl
EOF

FROM public.ecr.aws/docker/library/alpine:3.19.1@sha256:c5b1261d6d3e43071626931fc004f70149baeba2c8ec672bd4f27761f8e1ad6b AS kustomize-downloader
ARG TARGETOS
ARG TARGETARCH

RUN <<EOF
set -eu

apk update
apk add jq
URL=$(wget -qO- https://api.github.com/repos/kubernetes-sigs/kustomize/releases | jq -r '.[] | select(has("assets") and .assets != []) | .assets[].browser_download_url' | grep "${TARGETOS}_${TARGETARCH}" | head)
wget -qO- $URL | tar xz
EOF

FROM base AS runtime

ARG TARGETOS
ARG TARGETARCH

COPY --from=kubectl-downloader /kubectl /usr/local/bin/kubectl
COPY --from=kustomize-downloader /kustomize /usr/local/bin/kustomize

ENV BUILDKITE_AGENT_CONFIG=/buildkite/buildkite-agent.cfg

RUN mkdir -p /buildkite/builds /buildkite/hooks /buildkite/plugins \
    && curl -Lfs -o /usr/local/bin/ssh-env-config.sh https://raw.githubusercontent.com/buildkite/docker-ssh-env-config/master/ssh-env-config.sh \
    && chmod +x /usr/local/bin/ssh-env-config.sh

COPY ./buildkite-agent.cfg /buildkite/buildkite-agent.cfg
COPY ./buildkite-agent-$TARGETOS-$TARGETARCH /usr/local/bin/buildkite-agent
COPY ./entrypoint.sh /usr/local/bin/buildkite-agent-entrypoint

VOLUME /buildkite
ENTRYPOINT ["buildkite-agent-entrypoint"]
CMD ["start"]
