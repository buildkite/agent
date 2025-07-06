#!/usr/bin/env bash

# Must be executed after github-release.sh as it depends on release meta-data

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
  echo "Skipping Homebrew release, will happen in stable pipeline"
  exit 0
fi

if [[ "$CODENAME" == "stable" && "$IS_PRERELEASE" == "1" ]] ; then
  echo "Skipping Homebrew release, should have happened in unstable pipeline"
  exit 0
fi

GITHUB_RELEASE_VERSION=$(buildkite-agent meta-data get github_release_version)
GITHUB_RELEASE_TYPE=$(buildkite-agent meta-data get github_release_type)
GITHUB_RELEASE_ACCESS_TOKEN=$(aws ssm get-parameter --name /pipelines/agent/GITHUB_RELEASE_ACCESS_TOKEN --with-decryption --output text --query Parameter.Value --region us-east-1)

if [[ "$GITHUB_RELEASE_TYPE" != "stable" ]]; then
  BREW_RELEASE_TYPE="devel"
else
  BREW_RELEASE_TYPE="stable"
fi

BINARY_NAME_AMD64="buildkite-agent-darwin-amd64-${AGENT_VERSION}.tar.gz"
DOWNLOAD_URL_AMD64="https://github.com/buildkite/agent/releases/download/v$GITHUB_RELEASE_VERSION/$BINARY_NAME_AMD64"

BINARY_NAME_ARM64="buildkite-agent-darwin-arm64-${AGENT_VERSION}.tar.gz"
DOWNLOAD_URL_ARM64="https://github.com/buildkite/agent/releases/download/v$GITHUB_RELEASE_VERSION/$BINARY_NAME_ARM64"

ARTIFACTS_BUILD="$(buildkite-agent meta-data get "agent-artifacts-build")"

echo "--- :package: Calculating SHAs for releases/$BINARY_NAME_AMD64"

buildkite-agent artifact download  --build "$ARTIFACTS_BUILD" "releases/$BINARY_NAME_AMD64" .

# $ openssl dgst -sha256 -hex $FILE # portable sha256 with openssl
# SHA256($FILE)= 26ff51b51eab2bfbcb2796bc72feec366d7e37a6cf8a11686ee8a6f14a8fc92c
# | grep -o '\S*$' # grab the last word (some openssl versions only list the hex)
# 26ff51b51eab2bfbcb2796bc72feec366d7e37a6cf8a11686ee8a6f14a8fc92c
RELEASE_SHA256_AMD64="$(openssl dgst -sha256 -hex "releases/$BINARY_NAME_AMD64" | grep -o '\S*$')"

echo "Release SHA256: $RELEASE_SHA256_AMD64"

echo "--- :package: Calculating SHAs for releases/$BINARY_NAME_ARM64"

buildkite-agent artifact download  --build "$ARTIFACTS_BUILD" "releases/$BINARY_NAME_ARM64" .

# $ openssl dgst -sha256 -hex $FILE # portable sha256 with openssl
# SHA256($FILE)= 26ff51b51eab2bfbcb2796bc72feec366d7e37a6cf8a11686ee8a6f14a8fc92c
# | grep -o '\S*$' # grab the last word (some openssl versions only list the hex)
# 26ff51b51eab2bfbcb2796bc72feec366d7e37a6cf8a11686ee8a6f14a8fc92c
RELEASE_SHA256_ARM64="$(openssl dgst -sha256 -hex "releases/$BINARY_NAME_ARM64" | grep -o '\S*$')"

echo "Release SHA256: $RELEASE_SHA256_ARM64"

echo "--- :octocat: Fetching current Homebrew formula from GitHub Contents API"

FORMULA_FILE=./pkg/buildkite-agent.rb
UPDATED_FORMULA_FILE=./pkg/buildkite-agent-updated.rb

CONTENTS_API_RESPONSE="$(curl "https://api.github.com/repos/buildkite/homebrew-buildkite/contents/Formula/buildkite-agent.rb" -H "Authorization: token ${GITHUB_RELEASE_ACCESS_TOKEN}")"

echo "Base64 decoding GitHub response into $FORMULA_FILE"

mkdir -p pkg
parse_json '["content"]' <<< "$CONTENTS_API_RESPONSE" | openssl enc -base64 -d > "$FORMULA_FILE"

echo "--- :ruby: Updating formula file"

echo "Homebrew release type: $BREW_RELEASE_TYPE"
echo "Homebrew release version: $GITHUB_RELEASE_VERSION"
echo "Homebrew release amd64 download URL: $DOWNLOAD_URL_AMD64"
echo "Homebrew release amd64 download SHA256: $RELEASE_SHA256_AMD64"
echo "Homebrew release arm64 download URL: $DOWNLOAD_URL_ARM64"
echo "Homebrew release arm64 download SHA256: $RELEASE_SHA256_ARM64"

./scripts/update-homebrew-formula.rb \
  "$BREW_RELEASE_TYPE" "$GITHUB_RELEASE_VERSION" \
  "$DOWNLOAD_URL_AMD64" "$RELEASE_SHA256_AMD64" \
  "$DOWNLOAD_URL_ARM64" "$RELEASE_SHA256_ARM64" \
  < "$FORMULA_FILE" > "$UPDATED_FORMULA_FILE"

echo "--- :rocket: Commiting new formula to buildkite/homebrew-buildkite master branch via GitHub Contents API"

UPDATED_FORMULA_BASE64="$(openssl enc -base64 -A < "$UPDATED_FORMULA_FILE")"
MAIN_FORMULA_SHA="$(parse_json '["sha"]' <<< "$CONTENTS_API_RESPONSE")"

echo "Old formula SHA: $MAIN_FORMULA_SHA"

cat <<JSON > pkg/github_post_data.json
{
  "message": "buildkite-agent $GITHUB_RELEASE_VERSION",
  "sha": "$MAIN_FORMULA_SHA",
  "content": "$UPDATED_FORMULA_BASE64",
  "branch": "master"
}
JSON


if [[ "${DRY_RUN:-}" == "false" ]] ; then
  echo "Posting JSON to GitHub Contents API"
  curl -X PUT "https://api.github.com/repos/buildkite/homebrew-buildkite/contents/Formula/buildkite-agent.rb" \
      -H "Authorization: token ${GITHUB_RELEASE_ACCESS_TOKEN}" \
      -H "Content-Type: application/json" \
      --data-binary "@pkg/github_post_data.json" \
      --fail-with-body
else
  echo "Dry Run Mode: skipping commit on github"
fi
