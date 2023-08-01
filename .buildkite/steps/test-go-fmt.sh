#!/usr/bin/env bash

set -euo pipefail

echo --- :go: Checking go formatting
if [[ $(gofmt -l ./ | head -c 1 | wc -c) != 0 ]]; then
  echo "The following files haven't been formatted with \`go fmt\`:"
  gofmt -l ./
  echo
  echo "Fix this by running \`go fmt ./...\` locally, and committing the result."
  exit 1
fi

echo --- :go: Checking go mod tidyness
go mod tidy

if ! git diff --no-ext-diff --exit-code go.mod go.sum; then
  echo "The go.mod or go.sum files are out of sync with the source code"
  echo "Please run \`go mod tidy\` locally, and commit the result."
  exit 1
fi

echo +++ Everything is clean and tidy! 🎉
