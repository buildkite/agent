#!/usr/bin/env bash

# Must be executed after github-release.sh as it depends on release meta-data

set -eu

GITHUB_RELEASE_VERSION=$(buildkite-agent meta-data get github_release_version)
GITHUB_RELEASE_TYPE=$(buildkite-agent meta-data get github_release_type)

if [[ "$GITHUB_RELEASE_TYPE" != "stable" ]]; then
  BREW_RELEASE_TYPE="devel"
else
  BREW_RELEASE_TYPE="stable"
fi

# Allows you to pipe JSON in and fetch keys using Ruby hash syntax
#
# Examples:
#
#   echo '{"key":{"subkey": ["value"]}}' | parse_json '["key"]["subkey"].first'
function parse_json {
  ruby -rjson -e "print JSON.parse(\$<.read)$1"
}

BINARY_NAME=buildkite-agent-darwin-386.tar.gz
RELEASES_DIR=releases
RELEASE_ARTIFACT_PATH="$RELEASES_DIR/$BINARY_NAME"

DOWNLOAD_URL="https://github.com/buildkite/agent/releases/download/v$GITHUB_RELEASE_VERSION/$BINARY_NAME"
FORMULA_FILE=./pkg/buildkite-agent.rb
UPDATED_FORMULA_FILE=./pkg/buildkite-agent-updated.rb

echo "--- :package: Downloading artifacts from Github Release step"

echo "Release download URL: $DOWNLOAD_URL"

echo "Fetching release artifact"
buildkite-agent artifact download $RELEASE_ARTIFACT_PATH $RELEASES_DIR
RELEASE_SHA=($(shasum $RELEASE_ARTIFACT_PATH))

echo "Release SHA1: $RELEASE_SHA"

echo "--- :github: Fetching current homebrew formula from Github Contents API"

CONTENTS_API_RESPONSE=$(curl "https://api.github.com/repos/buildkite/homebrew-buildkite/contents/buildkite-agent.rb?access_token=$GITHUB_RELEASE_ACCESS_TOKEN")

echo "Base64 decoding Github response into $FORMULA_FILE"

echo $CONTENTS_API_RESPONSE | parse_json '["content"]' | base64 -d > $FORMULA_FILE

echo "--- :ruby: Updating formula file"

cat $FORMULA_FILE |
  ./scripts/utils/update-homebrew-formula.rb $BREW_RELEASE_TYPE $GITHUB_RELEASE_VERSION $DOWNLOAD_URL $RELEASE_SHA \
  > $UPDATED_FORMULA_FILE

echo "--- :rocket: Commiting new formula to master via Github Contents API"

UPDATED_FORMULA_BASE64=$(base64 $UPDATED_FORMULA_FILE)
MASTER_FORMULA_SHA=$(echo $CONTENTS_API_RESPONSE | parse_json '["sha"]')

echo "Old formula SHA: $MASTER_FORMULA_SHA"

echo "Posting JSON to Github Contents API"

curl -X PUT "https://api.github.com/repos/buildkite/homebrew-buildkite/contents/buildkite-agent.rb?access_token=$GITHUB_RELEASE_ACCESS_TOKEN" \
     -i \
     --fail \
     -d "{
       \"message\": \"buildkite-agent $GITHUB_RELEASE_VERSION\",
       \"sha\": \"$MASTER_FORMULA_SHA\",
       \"content\": \"$UPDATED_FORMULA_BASE64\",
       \"branch\": \"master\"
     }"
