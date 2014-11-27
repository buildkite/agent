#!/bin/bash
set -e

echo '--- Installing fpm'
gem install fpm
rbenv rehash

echo '--- Downloading Binaries'
rm -rf pkg
mkdir -p pkg
~/.buildbox/bin/buildbox-agent artifact download "pkg/*" pkg

./scripts/create_debian_package.sh
