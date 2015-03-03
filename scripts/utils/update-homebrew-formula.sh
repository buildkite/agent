#!/usr/bin/env bash

set -euo pipefail

# Allows you to pipe JSON in and fetch keys using Ruby hash syntax
#
# Examples:
#
#   echo '{"key":{"subkey": ["value"]}}' | parse_json '["key"]["subkey"].first'
function parse_json {
  ruby -rjson -e "print JSON.parse(\$<.read)$1"
}

BINARY_NAME=buildkite-agent-darwin-386.tar.gz
RELEASE_FILE="./releases/$BINARY_NAME"

if [[ ! -f $RELEASE_FILE ]]; then
  echo "Error: Missing file $RELEASE_FILE"
  exit 1
fi

DOWNLOAD_URL="https://github.com/buildkite/agent/releases/download/v$AGENT_VERSION/$BINARY_NAME"
FORMULA_FILE=./releases/buildkite-agent.rb
UPDATED_FORMULA_FILE=./releases/buildkite-agent-updated.rb

echo "Fetching master formula from Github Contents API"

CONTENTS_API_RESPONSE=$(curl "https://api.github.com/repos/buildkite/homebrew-buildkite/contents/buildkite-agent.rb?access_token=$GITHUB_RELEASE_ACCESS_TOKEN")

echo "Decoding into $FORMULA_FILE"

echo $CONTENTS_API_RESPONSE | parse_json '["content"]' | base64 -d > $FORMULA_FILE

echo "Writing updated formula to $UPDATED_FORMULA_FILE"

RELEASE_SHA=($(shasum $RELEASE_FILE))

cat $FORMULA_FILE |
  ./scripts/utils/update-homebrew-formula.rb $BREW_RELEASE_TYPE $AGENT_VERSION $DOWNLOAD_URL $RELEASE_SHA \
  > $UPDATED_FORMULA_FILE

echo "Updating master formula via Github Contents API"

UPDATED_FORMULA_BASE64=$(base64 $UPDATED_FORMULA_FILE)
MASTER_FORMULA_SHA=$(echo $CONTENTS_API_RESPONSE | parse_json '["sha"]')

curl -X PUT "https://api.github.com/repos/buildkite/homebrew-buildkite/contents/buildkite-agent.rb?access_token=$GITHUB_RELEASE_ACCESS_TOKEN" \
     -d "{
          \"message\": \"buildkite-agent $AGENT_VERSION\",
          \"sha\": \"$MASTER_FORMULA_SHA\",
          \"content\": \"$UPDATED_FORMULA_BASE64\",
          \"branch\": \"master\"
        }" \
     --fail
