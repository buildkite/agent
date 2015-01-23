#!/bin/bash
set -e

function publish() {
  echo "--- Building GitHub release for: $1"
}

# Export the function so we can use it in xargs
export -f build

echo '--- Downloading binaries'
rm -rf pkg
mkdir -p pkg
buildbox-agent artifact download "pkg/*" .

# Loop over all the .deb files and build them
ls pkg/* | xargs -I {} bash -c "build {}"

echo '--- 🚀'
# ruby scripts/publish_release.rb
