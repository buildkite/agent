FROM alpine:3.17.0 AS base

RUN apk update && apk add --no-cache \
    bash \
    curl \
    docker-cli \
    docker-cli-compose \
    docker-compose \
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

FROM base AS downloader

WORKDIR /downloads

RUN curl -fL "https://storage.googleapis.com/kubernetes-release/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" -o kubectl && \
    chmod +x kubectl

RUN curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh" | bash -s "/downloads"

FROM base AS runtime

COPY --from=downloader /downloads/kubectl /usr/local/bin/kubectl
COPY --from=downloader /downloads/kustomize /usr/local/bin/kustomize

ENV BUILDKITE_AGENT_CONFIG=/buildkite/buildkite-agent.cfg

RUN mkdir -p /buildkite/builds /buildkite/hooks /buildkite/plugins \
    && curl -Lfs -o /usr/local/bin/ssh-env-config.sh https://raw.githubusercontent.com/buildkite/docker-ssh-env-config/master/ssh-env-config.sh \
    && chmod +x /usr/local/bin/ssh-env-config.sh

COPY ./buildkite-agent.cfg /buildkite/buildkite-agent.cfg
COPY ./buildkite-agent /usr/local/bin/buildkite-agent
COPY ./entrypoint.sh /usr/local/bin/buildkite-agent-entrypoint

VOLUME /buildkite
ENTRYPOINT ["buildkite-agent-entrypoint"]
CMD ["start"]
