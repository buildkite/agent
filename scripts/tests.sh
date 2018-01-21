#!/bin/bash
set -euo pipefail
mkdir tmp/

echo "~~~ Installing test dependencies"
go get github.com/kyoh86/richgo
go get github.com/jstemmer/go-junit-report

echo '+++ Running tests'
go test -race ./... 2>&1 | tee tmp/output.txt | richgo testfilter

echo "~~~ Creating junit output"
go-junit-report < tmp/output.txt > "tmp/junit-${BUILDKITE_JOB_ID}.xml"
