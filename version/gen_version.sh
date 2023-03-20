#!/usr/bin/env bash

set -eufo pipefail

git describe --tags --abbrev=0 | sed 's/^v//' > VERSION
