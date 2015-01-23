#!/bin/bash
set -e

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing \$CODENAME (\`stable\` or \`unstable\`)"
  exit 1
fi

function build-package {
  echo "--- Building debian package $1/$2 ($CODENAME/$BUILDKITE_BUILD_NUMBER)"

  ./scripts/utils/build-debian-package.sh $1 $2 $BUILDKITE_BUILD_NUMBER
}

# Clear out any existing pkg dir
rm -rf pkg

echo '--- Installing dependencies'
bundle --path vendor/bundle
godep restore

# Build the packages
build-package "linux" "amd64"
build-package "linux" "386"
