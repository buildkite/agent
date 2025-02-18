#!/usr/bin/env bash
set -euo pipefail

go version
echo arch is "$(uname -m)"

go install gotest.tools/gotestsum@v1.8.0

echo '+++ Running tests'
gotestsum --junitfile "junit-${BUILDKITE_JOB_ID}.xml" -- -count=1 -coverprofile=cover.out -failfast "$@" ./...

echo 'Producing coverage report'
go tool cover -html cover.out -o cover.html
