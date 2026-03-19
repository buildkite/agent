#!/usr/bin/env sh

set -euf

cd api/proto

echo --- :buf: Installing buf...
go install github.com/bufbuild/buf/cmd/buf@v1.61.0
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10
go install connectrpc.com/connect/cmd/protoc-gen-connect-go@v1.19.1

echo --- :connectrpc: Checking protobuf file generation...
buf generate
if ! git diff --no-ext-diff --exit-code; then
  echo ^^^ +++
  echo "Generated protobuf files are out of sync with the source code"
  echo "Please run \`buf generate\` in the internal/proto directory locally, and commit the result."
  exit 1
fi
