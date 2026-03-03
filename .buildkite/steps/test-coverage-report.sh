#!/usr/bin/env bash
set -euo pipefail

echo 'Producing coverage report'
go tool covdata textfmt -i "$1" -o cover.out
go tool cover -html cover.out -o cover.html
