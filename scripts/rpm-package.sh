#!/bin/bash
set -e

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing \$CODENAME (stable or unstable)"
  exit 1
fi

echo '--- Getting agent version from build meta data'

export FULL_AGENT_VERSION=$(buildkite-agent meta-data get "agent-version-full")
export AGENT_VERSION=$(buildkite-agent meta-data get "agent-version")
export BUILD_VERSION=$(buildkite-agent meta-data get "agent-version-build")

echo "Full agent version: $FULL_AGENT_VERSION"
echo "Agent version: $AGENT_VERSION"
echo "Build version: $BUILD_VERSION"

YUM_PATH=/yum.buildkite.com

function build() {
  echo "--- Building rpm package $1/$2"

  BINARY_FILENAME="pkg/buildkite-agent-$1-$2"

  # Download the built binary artifact
  buildkite-agent artifact download $BINARY_FILENAME .

  # Make sure it's got execute permissions so we can extract the version out of it
  chmod +x $BINARY_FILENAME

  # Build the rpm package using the architectre and binary, they are saved to rpm/
  ./scripts/utils/build-linux-package.sh "rpm" $2 $BINARY_FILENAME $AGENT_VERSION $BUILD_VERSION
}

function publish() {
  echo "--- Creating yum repositories for $CODENAME/$1"

  ARCH_PATH="$YUM_PATH/buildkite-agent/$CODENAME/$1"
  mkdir -p $ARCH_PATH
  find "rpm/" -type f -name "*$1*" | xargs cp -t $ARCH_PATH
  createrepo $ARCH_PATH --database --unique-md-filenames
}

echo '--- Installing dependencies'
bundle

# Make sure we have a local copy of the yum repo
echo "--- Syncing s3://$RPM_S3_BUCKET to `hostname`"
mkdir -p $YUM_PATH
aws --region us-east-1 s3 sync "s3://$RPM_S3_BUCKET" "$YUM_PATH"

# Make sure we have a clean rpm folder
rm -rf rpm

# Build the packages into rpm/
build "linux" "amd64"
build "linux" "386"

# Move the files to the right places
publish "x86_64"
publish "i386"

# Sync back our changes to S3
echo "--- Syncing local $YUM_PATH changes back to s3://$RPM_S3_BUCKET"
aws --region us-east-1 s3 sync "$YUM_PATH/" "s3://$RPM_S3_BUCKET" --acl "public-read" --no-guess-mime-type --exclude "lost+found"
