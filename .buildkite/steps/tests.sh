#!/bin/bash

go version
echo arch is "$(uname -m)"

go install gotest.tools/gotestsum@v1.8.0

echo '+++ Running tests'
gotestsum --junitfile "junit-${OSTYPE}.xml" -- -count=1 "$@" ./... || TEST_EXIT_STATUS=$?

echo '+++ Running integration tests for git-mirrors experiment'
TEST_EXPERIMENT=git-mirrors gotestsum --junitfile "junit-${OSTYPE}-git-mirrors.xml" -- -count=1 "$@" ./bootstrap/integration || TEST_EXIT_STATUS_WITH_MIRROS=$?

echo '+++ Test Analytics'
if [[ -n "${TEST_ANALYTICS_TOKEN_ENV_KEY-}" ]]; then
  cat "junit-${OSTYPE}.xml" | TEST_ANALYTICS_TOKEN=${!TEST_ANALYTICS_TOKEN_ENV_KEY} bash -c "`curl -sL https://raw.githubusercontent.com/buildkite/collector-junit/main/collect.sh`"
else
  echo "No TEST_ANALYTICS_TOKEN_ENV_KEY is present, skipping..."
fi

if [[ $TEST_EXIT_STATUS -ne 0 ]]; then
  exit $TEST_EXIT_STATUS
elif [[ $TEST_EXIT_STATUS_WITH_MIRROS -ne 0 ]]; then
  exit $TEST_EXIT_STATUS_WITH_MIRROS
fi
