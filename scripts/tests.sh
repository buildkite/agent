#!/bin/bash
set -euo pipefail
go test -race ./... |& sed -e 's/^---/***/'
