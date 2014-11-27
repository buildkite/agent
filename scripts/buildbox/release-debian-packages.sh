#!/bin/bash
set -e

echo '--- Installing deb-s3'
gem install deb-s3
rbenv rehash

echo '--- Downloading debian packages'
~/.buildbox/bin/buildbox-agent artifact download "pkg/*.deb" . --job ""

./scripts/publish_debian_package.sh "buildbox-agent_1.0.0_386.deb"
./scripts/publish_debian_package.sh "buildbox-agent_1.0.0_amd64.deb"
