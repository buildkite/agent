#!/usr/bin/env bash

set -Eeufo pipefail

echo --- :golang: Generating code
go generate ./...

echo --- :git: Checking generated code matches commit
if ! git diff --exit-code; then
  echo +++ :x: Generated code was not commited.
  echo "Run"
  echo "  go generate ./..."
  echo "and make a commit."

  exit 1
fi
