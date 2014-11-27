#!/bin/bash
set -e

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing $CODENAME (`stable` or `unstable`)"
  exit 1
fi

echo '--- Installing deb-s3'
gem install deb-s3
rbenv rehash

echo '--- Downloading debian packages'
~/.buildbox/bin/buildbox-agent artifact download "pkg/*.deb" . --job ""

echo '--- Uploading packages'
ls pkg/*.deb | xargs -0 -I {} ./scripts/publish_debian_package.sh {} $CODENAME
