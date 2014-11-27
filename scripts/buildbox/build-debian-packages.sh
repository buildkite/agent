#!/bin/bash
set -e

echo '--- building packages'
./script/create_debian_package.sh

echo '--- download binaries'
rm -rf pkg
mkdir -p pkg
buildbox-artifact download "pkg/*" pkg
