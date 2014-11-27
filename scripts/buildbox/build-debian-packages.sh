#!/bin/bash
set -e

echo '--- Downloading Binaries'
rm -rf pkg
mkdir -p pkg
buildbox-agent artifact download "pkg/*" pkg

./scripts/create_debian_package.sh
