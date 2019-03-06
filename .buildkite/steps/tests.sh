#!/bin/bash
set -euo pipefail

GO111MODULE=off get go get gotest.tools/gotestsum

echo '+++ Running tests'
gotestsum --junitfile "junit-${OSTYPE}.xml" ./...
