#!/bin/bash
set -e
set -x

echo $PATH
whoami
env

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing \$CODENAME (stable or unstable)"
  exit 1
fi

YUM_TMP_PATH=/var/tmp/buildkite-agent-yum-repo

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

function publish() {
  echo "--- Creating yum repositories for $CODENAME/$1"

  ARCH_PATH="$YUM_TMP_PATH/buildkite-agent/$CODENAME/$1"
  mkdir -p $ARCH_PATH
  find "rpm/" -type f -name "*$1*" | xargs cp -t $ARCH_PATH
  createrepo $ARCH_PATH --database --unique-md-filenames
}

function sync() {
  echo "--- Syncing s3://$RPM_S3_BUCKET"

  mkdir -p $YUM_TMP_PATH
  s3cmd sync $YUM_TMP_PATH/ "s3://$RPM_S3_BUCKET" --acl-public --verbose --no-guess-mime-type
}

echo '--- Installing dependencies'
bundle

# Make sure we have a clean rpm folder and cache folder
rm -rf $YUM_TMP_PATH
rm -rf rpm

# Build the packages into rpm/
build "linux" "amd64"
build "linux" "386"

# Make sure we have a local copy of the yum repo
sync

# Move the filees to the right places
publish "x86_64"
publish "i386"

# Sync back our changes to S3
sync
