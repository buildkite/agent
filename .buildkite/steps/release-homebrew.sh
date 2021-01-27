#!/usr/bin/env bash

# Must be executed after github-release.sh as it depends on release meta-data

set -euo pipefail

# Allows you to pipe JSON in and fetch keys using Ruby hash syntax
#
# Examples:
#
#   echo '{"key":{"subkey": ["value"]}}' | parse_json '["key"]["subkey"].first'
function parse_json {
  ruby -r json -e "print JSON.parse(STDIN.read)$1"
}

function to_json {
  ruby -r json -e "print STDIN.read.to_json"
}

echo '--- Getting agent version from build meta data'

export FULL_AGENT_VERSION=$(buildkite-agent meta-data get "agent-version-full")
export AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")
export BUILD_VERSION=$(buildkite-agent meta-data get "agent-version-build")
export IS_PRERELEASE=$(buildkite-agent meta-data get "agent-is-prerelease")

echo "Full agent version: $FULL_AGENT_VERSION"
echo "Agent version: $AGENT_VERSION"
echo "Build version: $BUILD_VERSION"
echo "Is prerelease?: $IS_PRERELEASE"

if [[ "$CODENAME" == "unstable" && "$IS_PRERELEASE" == "0" ]] ; then
  echo "Skipping homebrew release, will happen in stable pipeline"
  exit 0
fi

if [[ "$CODENAME" == "stable" && "$IS_PRERELEASE" == "1" ]] ; then
  echo "Skipping homebrew release, should have happened in unstable pipeline"
  exit 0
fi

GITHUB_RELEASE_VERSION=$(buildkite-agent meta-data get github_release_version)
GITHUB_RELEASE_TYPE=$(buildkite-agent meta-data get github_release_type)

if [[ "$GITHUB_RELEASE_TYPE" != "stable" ]]; then
  BREW_RELEASE_TYPE="devel"
else
  BREW_RELEASE_TYPE="stable"
fi

BINARY_ARCH="amd64"
BINARY_NAME="buildkite-agent-darwin-${BINARY_ARCH}-${AGENT_VERSION}.tar.gz"

DOWNLOAD_URL="https://github.com/buildkite/agent/releases/download/v$GITHUB_RELEASE_VERSION/$BINARY_NAME"
FORMULA_FILE=./pkg/buildkite-agent.rb
UPDATED_FORMULA_FILE=./pkg/buildkite-agent-updated.rb

ARTIFACTS_BUILD="$(buildkite-agent meta-data get "agent-artifacts-build")"

echo "--- :package: Calculating SHAs for releases/$BINARY_NAME"

buildkite-agent artifact download  --build "$ARTIFACTS_BUILD" "releases/$BINARY_NAME" .

# $ openssl dgst -sha256 -hex $FILE # portable sha256 with openssl
# SHA256($FILE)= 26ff51b51eab2bfbcb2796bc72feec366d7e37a6cf8a11686ee8a6f14a8fc92c
# | grep -o '\S*$' # grab the last word (some openssl versions only list the hex)
# 26ff51b51eab2bfbcb2796bc72feec366d7e37a6cf8a11686ee8a6f14a8fc92c
RELEASE_SHA256="$(openssl dgst -sha256 -hex "releases/$BINARY_NAME" | grep -o '\S*$')"

echo "Release SHA256: $RELEASE_SHA256"

echo "--- :octocat: Fetching current homebrew formula from Github Contents API"

CONTENTS_API_RESPONSE="$(curl "https://api.github.com/repos/buildkite/homebrew-buildkite/contents/buildkite-agent.rb?access_token=$GITHUB_RELEASE_ACCESS_TOKEN")"

echo "Base64 decoding Github response into $FORMULA_FILE"

mkdir -p pkg
parse_json '["content"]' <<< "$CONTENTS_API_RESPONSE" | openssl enc -base64 -d > "$FORMULA_FILE"

echo "--- :ruby: Updating formula file"

echo "Homebrew release type: $BREW_RELEASE_TYPE"
echo "Homebrew release version: $GITHUB_RELEASE_VERSION"
echo "Homebrew release download URL: $DOWNLOAD_URL"
echo "Homebrew release download SHA256: $RELEASE_SHA256"

./scripts/update-homebrew-formula.rb "$BREW_RELEASE_TYPE" "$GITHUB_RELEASE_VERSION" "$DOWNLOAD_URL" "$RELEASE_SHA256" < "$FORMULA_FILE" > "$UPDATED_FORMULA_FILE"

echo "--- :rocket: Commiting new formula to master via Github Contents API"

UPDATED_FORMULA_BASE64="$(openssl enc -base64 -A < "$UPDATED_FORMULA_FILE")"
MASTER_FORMULA_SHA="$(parse_json '["sha"]' <<< "$CONTENTS_API_RESPONSE")"

echo "Old formula SHA: $MASTER_FORMULA_SHA"

cat <<JSON > pkg/github_post_data.json
{
  "message": "buildkite-agent $GITHUB_RELEASE_VERSION",
  "sha": "$MASTER_FORMULA_SHA",
  "content": "$UPDATED_FORMULA_BASE64",
  "branch": "master"
}
JSON


if [[ "${DRY_RUN:-}" == "false" ]] ; then
  echo "Posting JSON to Github Contents API"
  curl -X PUT "https://api.github.com/repos/buildkite/homebrew-buildkite/contents/buildkite-agent.rb?access_token=$GITHUB_RELEASE_ACCESS_TOKEN" \
      -H "Content-Type: application/json" \
      --data-binary "@pkg/github_post_data.json" \
      --fail
fi
