#!/usr/bin/env sh

set -euf

export PATH="${GOBIN}:${PATH}"

install_go_tool() {
  binary="$1"
  package="$2"
  version="$3"

  if [ -x "${GOBIN}/${binary}" ]; then
    echo "Using cached ${binary}"
    return
  fi

  go install "${package}@${version}"
}

cd api/proto

echo --- :buf: Installing buf...
mkdir -p "${GOBIN}"
install_go_tool buf github.com/bufbuild/buf/cmd/buf "${PROTOBUF_BUF_VERSION}"
install_go_tool protoc-gen-go google.golang.org/protobuf/cmd/protoc-gen-go "${PROTOBUF_PROTOC_GEN_GO_VERSION}"
install_go_tool protoc-gen-connect-go connectrpc.com/connect/cmd/protoc-gen-connect-go "${PROTOBUF_PROTOC_GEN_CONNECT_GO_VERSION}"

echo --- :connectrpc: Checking protobuf file generation...
buf generate
if ! git diff --no-ext-diff --exit-code; then
  echo ^^^ +++
  echo "Generated protobuf files are out of sync with the source code"
  echo "Please run \`buf generate\` in the api/proto directory locally, and commit the result."
  exit 1
fi
