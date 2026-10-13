#!/usr/bin/env bash
# EXPERIMENT: runs a CI step script directly in the agent container on the
# kube-operator standard queues, which have no Docker. buildkite/agent:4 has no
# Go toolchain, so install the versions CI's images use with mise. Persistent
# runners keep them between jobs.
set -euo pipefail

export PATH="${HOME}/.local/bin:${PATH}"
if ! command -v mise >/dev/null; then
  echo "--- :hammer_and_wrench: Installing mise"
  curl -fsSL https://mise.run | sh
fi

# The polyglot hook tests need Ruby. Alpine's package is much quicker than
# building it with mise.
if ! command -v ruby >/dev/null && [[ "$(id -u)" == 0 ]] && command -v apk >/dev/null; then
  echo "--- :ruby: Installing Ruby"
  apk add --no-cache ruby >/dev/null
fi

go_version="$(sed -n 's/^go //p' go.mod)"
golangci_lint_version="$(sed -n 's|^FROM golangci/golangci-lint:v\([0-9.]*\)-.*|\1|p' .buildkite/Dockerfile-lint)"

# Skip the repo's mise.toml: it pins an older golangci-lint and builds Ruby.
export MISE_IGNORED_CONFIG_PATHS="${PWD}/mise.toml"

echo "--- :go: Installing go ${go_version} and golangci-lint ${golangci_lint_version}"
mise install --quiet "go@${go_version}" "golangci-lint@${golangci_lint_version}"
exec mise exec "go@${go_version}" "golangci-lint@${golangci_lint_version}" -- "$@"
