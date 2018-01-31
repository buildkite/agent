#!/bin/bash
set -euo pipefail
go test -v ./... |& sed -e 's/^---/***/'
