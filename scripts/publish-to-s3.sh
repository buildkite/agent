#!/usr/bin/env bash

set -eo pipefail

version=$(buildkite-agent meta-data get "agent-version")
build=$(buildkite-agent meta-data get "agent-version-build")

if [[ "$CODENAME" == "experimental" ]]; then
  version="$version.$build"
fi

echo "--- :package: Downloading built binaries"

rm -rf pkg/*
buildkite-agent artifact download "pkg/*" .
cd pkg

echo "--- :s3: Publishing $version to download.buildkite.com"

s3_base_url="s3://download.buildkite.com/agent/$CODENAME"

for binary in buildkite-agent-*; do
  binary_s3_url="$s3_base_url/$version/$binary"

  echo "Publishing $binary to $binary_s3_url"
  aws s3 cp --acl "public-read" "$binary" "$binary_s3_url"
done

echo "--- :s3: Updating latest"

latest_version=$(aws s3 ls --page-size 1000 "$s3_base_url/" | grep PRE | awk '{print $2}' | awk -F '/' '{print $1}' | ruby -pe 'readlines.map {|s| Gem::Version.new(s)}.max')

latest_version_s3_url="$s3_base_url/$latest_version/"
latest_s3_url="$s3_base_url/latest/"

echo "Copying $latest_version_s3_url to $latest_s3_url"

aws s3 cp --acl public-read-write --recursive "$latest_version_s3_url" "$latest_s3_url"

echo "--- :tada: All done!"
