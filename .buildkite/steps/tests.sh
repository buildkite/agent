#!/bin/bash
set -euo pipefail

go version
echo arch is "$(uname -m)"

export CGO_ENABLED=0
go install gotest.tools/gotestsum@v1.8.0

echo '+++ Running tests'
gotestsum --junitfile "junit-${BUILDKITE_JOB_ID}.xml" -- -count=1 -failfast -race ./...

echo '+++ Running integration tests for git-mirrors experiment'
TEST_EXPERIMENT=git-mirrors gotestsum --junitfile "junit-${BUILDKITE_JOB_ID}-git-mirrors.xml" -- -count=1 -failfast -race ./bootstrap/integration
