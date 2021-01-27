#!/bin/bash
set -euo pipefail

echo '+++ Running tests'
go test -race ./... 2>&1 | sed -e 's/^---/***/'
