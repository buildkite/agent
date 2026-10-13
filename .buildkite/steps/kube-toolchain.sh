#!/usr/bin/env bash
# EXPERIMENT: runs a CI step script with the toolchain CI's images provide, on
# the kube-operator queues. Standard queues run it in the agent container
# (buildkite/agent:4, Alpine); Docker queues run it in the step's image:
# (golang, Debian). Neither has golangci-lint, so install the version
# Dockerfile-lint uses with mise, and Go too if the image's doesn't match.
set -euo pipefail

export PATH="${HOME}/.local/bin:${PATH}"
if ! command -v mise >/dev/null; then
  echo "--- :hammer_and_wrench: Installing mise"
  curl -fsSL https://mise.run | sh
fi

# The polyglot hook tests need Ruby, which Dockerfile-compile installs, and
# the race detector needs cgo.
apk=()
apt=()
if [[ "${KUBE_TOOLCHAIN_RUBY:-}" == "true" ]] && ! command -v ruby >/dev/null; then
  apk+=(ruby)
  apt+=(ruby)
fi
if [[ "${KUBE_TOOLCHAIN_CGO:-}" == "true" ]] && ! command -v gcc >/dev/null; then
  apk+=(gcc musl-dev)
  apt+=(gcc libc6-dev)
fi
if (( ${#apk[@]} > 0 )); then
  echo "--- :package: Installing ${apk[*]}"
  if command -v apk >/dev/null; then
    apk add --no-cache "${apk[@]}" >/dev/null
  else
    apt-get update -qq && apt-get install -y -qq "${apt[@]}" >/dev/null
  fi
fi

# In a container, the git host alias test runs and needs these SSH aliases,
# which Dockerfile-compile also installs.
if [[ -f /.dockerenv && -d /etc/ssh/ssh_config.d ]]; then
  cp .buildkite/build/ssh.conf /etc/ssh/ssh_config.d/
fi

go_version="$(sed -n 's/^go //p' go.mod)"
golangci_lint_version="$(sed -n 's|^FROM golangci/golangci-lint:v\([0-9.]*\)-.*|\1|p' .buildkite/Dockerfile-lint)"
tools=("golangci-lint@${golangci_lint_version}")
if [[ "$(go env GOVERSION 2>/dev/null)" != "go${go_version}" ]]; then
  tools+=("go@${go_version}")
fi

# Skip the repo's mise.toml: it pins an older golangci-lint and builds Ruby.
export MISE_IGNORED_CONFIG_PATHS="${PWD}/mise.toml"

echo "--- :go: Installing ${tools[*]}"
mise install --quiet "${tools[@]}"
exec mise exec "${tools[@]}" -- "$@"
