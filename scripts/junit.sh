#!/bin/bash
set -euo pipefail
mkdir -p tmp

echo "--- :junit: Download the junits"
buildkite-agent artifact download tmp/junit-*.xml tmp

echo "--- :junit::golang: Processing the junits"
docker run --rm -v "${PWD}:${PWD}" -w "$PWD" gugahoi/junit-report-converter tmp/junit-*.xml > tmp/annotation.md

echo "--- :buildkite: Creating annotation"
buildkite-agent annotate --context junit --style error < tmp/annotation.md
