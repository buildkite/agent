#!/bin/bash
set -e

artifacts_build=$(buildkite-agent meta-data get \
  "agent-artifacts-build" --default value "$BUILDKITE_BUILD_ID")

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing \$CODENAME (stable or unstable)"
  exit 1
fi

echo '--- Downloading built debian packages'
rm -rf deb
mkdir -p deb
buildkite-agent artifact download --build "$artifacts_build" "deb/*.deb" deb/

echo '--- Installing dependencies'
bundle

# Loop over all the .deb files and publish them
for file in deb/*.deb; do
  echo "+++ Publishing $file"
  echo ./scripts/utils/publish-debian-package.sh "$file" "$CODENAME"
done
