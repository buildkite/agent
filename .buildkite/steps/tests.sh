#!/bin/bash
set -euo pipefail

GO111MODULE=off go get gotest.tools/gotestsum

echo '+++ Running tests'
gotestsum --junitfile "junit-${OSTYPE}.xml" -- -count=1 -failfast ./...
