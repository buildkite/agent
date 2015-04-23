#!/bin/bash
set -e

echo $PATH
whoami
env

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing \$CODENAME (stable or unstable)"
  exit 1
fi

function build() {
  echo "--- Building rpm package $1/$2"

  BINARY_FILENAME="pkg/buildkite-agent-$1-$2"

  # Download the built binary artifact
  buildkite-agent artifact download $BINARY_FILENAME . --job ""

  # Make sure it's got execute permissions so we can extract the version out of it
  chmod +x $BINARY_FILENAME

  # Build the rpm package using the architectre and binary, they are saved to rpm/
  ./scripts/utils/build-linux-package.sh "rpm" $2 $BINARY_FILENAME
}

echo '--- Installing dependencies'
bundle

# Make sure we have a clean rpm folder
rm -rf rpm

# Build the packages into rpm/
build "linux" "amd64"
build "linux" "386"

YUM_TMP_PATH=/var/tmp/buildkite-agent-yum-repo

echo "--- Downlading yum repository"

mkdir -p $YUM_TMP_PATH
cd $YUM_TMP_PATH
s3cmd sync $YUM_TMP_PATH "s3://$RPM_S3_BUCKET" --acl-public --verbose --no-guess-mime-type

echo "--- Creating yum repositories for $CODENAME/amd64"

ARCH_PATH="$YUM_TMP_PATH/buildkite-agent/rpm/amd64/$CODENAME"
mkdir -p $ARCH_PATH
find rpm -type f -name "amd64" | xargs cp -t $ARCH_PATH
createrepo $ARCH_PATH --database --unique-md-filenames

echo "--- Creating yum repositories for $CODENAME/386"

ARCH_PATH="$YUM_TMP_PATH/buildkite-agent/rpm/386/$CODENAME"
mkdir -p $ARCH_PATH
find rpm -type f -name "386" | xargs cp -t $ARCH_PATH
createrepo $ARCH_PATH --database --unique-md-filenames

echo "--- Syncing to S3"

s3cmd sync $YUM_TMP_PATH "s3://$RPM_S3_BUCKET" --acl-public --verbose --no-guess-mime-type
