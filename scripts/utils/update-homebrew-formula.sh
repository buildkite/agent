#!/bin/bash
set -e

if [[ "$GITHUB_RELEASE_ACCESS_TOKEN" == "" ]]; then
  echo "Error: Missing \$GITHUB_RELEASE_ACCESS_TOKEN"
  exit 1
fi

if [[ "$AGENT_VERSION" == "" ]]; then
  echo "Error: Missing \$AGENT_VERSION"
  exit 1
fi

if [[ "$RELEASE_TYPE" == "" ]]; then
  echo "Error: Missing \$RELEASE_TYPE"
  exit 1
fi

# Check and SHA the release

RELEASE_FILE=./releases/buildkite-agent-darwin-386.tar.gz

if [[ ! -f $RELEASE_FILE ]]; then
  echo "Error: Missing file $RELEASE_FILE"
  exit 1
fi

RELEASE_SHA=($(shasum $RELEASE_FILE))

# Grab the formula from master

FORMULA_FILE=./releases/buildkite-agent.rb

curl https://raw.githubusercontent.com/buildkite/homebrew-buildkite/master/buildkite-agent.rb -o $FORMULA_FILE

MASTER_FORMULA_SHA=($(shasum $FORMULA_FILE))

# Update the homebrew formula

# The formula has these sections, which need to be updated:
#
#   stable do
#     version "..."
#     url     "..."
#     sha1    "..."
#   end
#
#   devel do
#     version "..."
#     url     "..."
#     sha1    "..."
#   end

# TODO: Update the formula

NEW_FORMULA_BASE64=$(base64 $FORMULA_FILE)

# Update Github

curl -X PUT https://api.github.com/repos/buildkite/homebrew-buildkite/contents/buildkite-agent.rb \
     -d "message=buildkite-agent $AGENT_VERSION" \
     -d "sha=$MASTER_FORMULA_SHA" \
     -d "content=$NEW_FORMULA_BASE64" \
     -d "branch=master" \
     -d "access_token=$GITHUB_RELEASE_ACCESS_TOKEN"