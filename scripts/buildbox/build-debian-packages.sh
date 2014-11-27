#!/bin/bash
set -e

echo '--- download binaries'
rm -rf pkg
mkdir -p pkg
buildbox-artifact download "pkg/*" pkg
