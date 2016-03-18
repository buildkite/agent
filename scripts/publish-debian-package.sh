#!/bin/bash
set -e

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing \$CODENAME (stable or unstable)"
  exit 1
fi

echo '--- Downloading built debian packages'
rm -rf deb
mkdir -p deb
buildkite-agent artifact download "deb/*.deb" deb/

echo '--- Installing dependencies'
bundle

# Loop over all the .deb files and publish them
for file in deb/*.deb; do
  echo "+++ Shipping $file"
  ./scripts/utils/publish-debian-package.sh $file $CODENAME
done
