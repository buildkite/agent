#!/bin/bash
set -e

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing \$CODENAME (\`stable\` or \`unstable\`)"
  exit 1
fi

function build-package {
  # Attach the Buildkite build number to debian packages we're releasing to the
  # unstable chanel.
  if [[ "$CODENAME" == "unstable" ]]; then
    echo "--- Building debian package $1/$2 ($CODENAME/$BUILDKITE_BUILD_NUMBER)"

    ./scripts/utils/build-debian-package.sh $1 $2 $BUILDKITE_BUILD_NUMBER
  else
    echo "--- Building debian package $1/$2 ($CODENAME)"

    ./scripts/utils/build-debian-package.sh $1 $2
  fi
}

# Clear out any existing pkg dir
rm -rf pkg

echo '--- Installing dependencies'
ls -lsa
ls vendor -lsa
ls vendor/bundle -lsa
bundle --path vendor/bundle
godep restore

# Build the packages
build-package "linux" "amd64"
build-package "linux" "386"
