#!/bin/bash
set -e

echo '--- Installing fpm'
gem install fpm

echo '--- Downloading Binaries'
rm -rf pkg
mkdir -p pkg
buildbox-agent artifact download "pkg/*" pkg

./scripts/create_debian_package.sh
