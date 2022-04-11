#!/bin/bash
set -euo pipefail

go version
echo arch is "$(uname -m)"

GO111MODULE=off go get gotest.tools/gotestsum

echo '+++ Running tests'
gotestsum --junitfile "junit-${OSTYPE}.xml" -- -count=1 -failfast "$@" ./...

echo '+++ Running integration tests for git-mirrors experiment'
TEST_EXPERIMENT=git-mirrors gotestsum --junitfile "junit-${OSTYPE}-git-mirrors.xml" -- -count=1 -failfast "$@" ./bootstrap/integration
