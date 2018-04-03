#!/bin/bash
set -euo pipefail

artifacts_build=$(buildkite-agent meta-data get "agent-artifacts-build")

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing \$CODENAME (stable or unstable)"
  exit 1
fi

YUM_PATH=/yum.buildkite.com

function publish() {
  echo "--- Creating yum repositories for $CODENAME/$1"

  ARCH_PATH="$YUM_PATH/buildkite-agent/$CODENAME/$1"
  mkdir -p $ARCH_PATH
  find "rpm/" -type f -name "*$1*" | xargs cp -t "$ARCH_PATH"
  # createrepo_c is much faster and more resilient than createrepo
  createrepo_c --no-database --unique-md-filenames --retain-old-md-by-age=7d --update "$ARCH_PATH" || \
    createrepo_c --no-database --unique-md-filenames --retain-old-md-by-age=7d "$ARCH_PATH"
  #createrepo --no-database --unique-md-filenames --update "$ARCH_PATH" || \
  #  createrepo --no-database --unique-md-filenames "$ARCH_PATH"
}

echo '--- Downloading built yum packages packages'
rm -rf rpm
mkdir -p rpm
buildkite-agent artifact download --build "$artifacts_build" "rpm/*.rpm" rpm/

echo '--- Installing dependencies'
bundle

# Make sure we have a local copy of the yum repo
echo "--- Syncing s3://$RPM_S3_BUCKET to `hostname`"
mkdir -p $YUM_PATH
dry_run aws --region us-east-1 s3 sync --delete "s3://$RPM_S3_BUCKET" "$YUM_PATH"

# Move the files to the right places
dry_run publish "x86_64"
dry_run publish "i386"

# Sync back our changes to S3
echo "--- Syncing local $YUM_PATH changes back to s3://$RPM_S3_BUCKET"
dry_run aws --region us-east-1 s3 sync --delete "$YUM_PATH/" "s3://$RPM_S3_BUCKET" --no-guess-mime-type --exclude "lost+found" --exclude ".repodata"
