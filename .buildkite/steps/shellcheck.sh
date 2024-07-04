#!/usr/bin/env sh

set -euo

find . -type f -name "*.sh" -print0 | xargs shellcheck -S info
