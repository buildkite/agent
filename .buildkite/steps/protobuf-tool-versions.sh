#!/usr/bin/env sh

# These versions are used both by go install and by the protobuf tools cache
# key, keeping cache invalidation aligned with the requested binaries.
export PROTOBUF_BUF_VERSION=v1.61.0
export PROTOBUF_PROTOC_GEN_GO_VERSION=v1.36.10
export PROTOBUF_PROTOC_GEN_CONNECT_GO_VERSION=v1.19.1
