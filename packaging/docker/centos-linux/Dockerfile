FROM centos:8.3.2011

ARG TARGETARCH

ENV DOCKER_COMPOSE_VERSION=v2.0.0-rc.3
ENV TINI_VERSION=0.19.0

RUN yum -y install \
      bash \
      ca-certificates \
      crontabs \
      curl \
      epel-release \
      git \
      jq \
      openssh-clients \
      perl \
      rsync \
      yum-utils \
    && yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo \
    && yum -y install docker-ce-cli \
    && yum clean all \
    && rm -rf /var/cache/yum*

RUN curl -Lfs -o /sbin/tini  https://github.com/krallin/tini/releases/download/v${TINI_VERSION}/tini-${TARGETARCH} \
    && chmod +x /sbin/tini \
    && curl -Lfs https://github.com/docker/compose/releases/download/${DOCKER_COMPOSE_VERSION}/docker-compose-linux-${TARGETARCH} -o /usr/local/bin/docker-compose \
    && chmod +x /usr/local/bin/docker-compose

ENV BUILDKITE_AGENT_CONFIG=/buildkite/buildkite-agent.cfg \
    PATH="/usr/local/bin:${PATH}"

RUN mkdir -p /buildkite/builds /buildkite/hooks /buildkite/plugins \
    && curl -Lfs -o /usr/local/bin/ssh-env-config.sh https://raw.githubusercontent.com/buildkite/docker-ssh-env-config/master/ssh-env-config.sh \
    && chmod +x /usr/local/bin/ssh-env-config.sh

COPY ./buildkite-agent.cfg /buildkite/buildkite-agent.cfg
COPY ./buildkite-agent-linux-${TARGETARCH} /usr/local/bin/buildkite-agent
COPY ./entrypoint.sh /usr/local/bin/buildkite-agent-entrypoint

VOLUME /buildkite
ENTRYPOINT ["buildkite-agent-entrypoint"]
CMD ["start"]
