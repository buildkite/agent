#!/bin/bash
set -e

echo '--- Installing deb-s3'
gem install deb-s3
rbenv rehash

echo '--- Downloading debian packages'
~/.buildbox/bin/buildbox-agent artifact download "pkg/*.deb" . --job ""
