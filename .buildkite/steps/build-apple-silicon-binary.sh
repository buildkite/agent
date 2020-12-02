#!/bin/bash

set -euo pipefail

echo "--- :${1}: Building ${1}/${2}"

rm -rf pkg

go get golang.org/dl/gotip

gotip download 272258

gotip version

./scripts/build-binary.sh "${1}" "${2}" "${BUILDKITE_BUILD_NUMBER}"
