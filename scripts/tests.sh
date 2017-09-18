#!/bin/bash

set -euo pipefail

echo '+++ Running tests'

go get -u github.com/lox/bintest
go test $(go list ./... | grep -v /vendor/)
