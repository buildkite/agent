#!/usr/bin/env bash

set -euo pipefail

BINARY_NAME=buildkite-agent-darwin-386.tar.gz
RELEASE_FILE="./releases/$BINARY_NAME"

if [[ ! -f $RELEASE_FILE ]]; then
  echo "Error: Missing file $RELEASE_FILE"
  exit 1
fi

DOWNLOAD_URL="https://github.com/buildkite/agent/releases/download/v$AGENT_VERSION/$BINARY_NAME"
FORMULA_FILE=./releases/buildkite-agent.rb
UPDATED_FORMULA_FILE=./releases/buildkite-agent-updated.rb

# Grab the formula from master

curl https://raw.githubusercontent.com/buildkite/homebrew-buildkite/master/buildkite-agent.rb -o $FORMULA_FILE

# Update the homebrew formula

RELEASE_SHA=($(shasum $RELEASE_FILE))

cat $FORMULA_FILE |
  ./scripts/utils/update-homebrew-formula.rb $BREW_RELEASE_TYPE $AGENT_VERSION $DOWNLOAD_URL $RELEASE_SHA \
  > $UPDATED_FORMULA_FILE

# Update Github

UPDATED_FORMULA_BASE64=$(base64 $UPDATED_FORMULA_FILE)
MASTER_FORMULA_SHA=($(shasum $FORMULA_FILE))

curl -X PUT https://api.github.com/repos/buildkite/homebrew-buildkite/contents/buildkite-agent.rb \
     -d "message=buildkite-agent $AGENT_VERSION" \
     -d "sha=$MASTER_FORMULA_SHA" \
     -d "content=$UPDATED_FORMULA_BASE64" \
     -d "branch=master" \
     -d "access_token=$GITHUB_RELEASE_ACCESS_TOKEN"
