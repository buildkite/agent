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

echo '--- Downloading built yum packages'
rm -rf rpm
mkdir -p rpm
buildkite-agent artifact download --build "$artifacts_build" "rpm/*.rpm" rpm/

echo '--- Installing dependencies'
# currently, the libcrypto3-3.0.5-r0 in the alpine/v3.16/main repo isn't compatible with createrepo_c:
#     ERROR: unable to select packages:
#       so:libcrypto.so.3 (no such package):
#         required by: createrepo_c-libs-0.17.1-r2[so:libcrypto.so.3]
# So, we add alpine/edge/main for libcrypto3 and alpine/edge/testing for createrepo_c.
# In future we can probably remove the explicit libcrypto3 install, and the alpine/edge/main repo.
apk add --update-cache --no-progress \
  --repository=http://dl-cdn.alpinelinux.org/alpine/edge/main \
  --repository=http://dl-cdn.alpinelinux.org/alpine/edge/testing \
  'libcrypto3>=3.0.5-r2' \
  createrepo_c

# createrepo_c requires some exotic flags on the cp, which aren't available on the busybox version
apk add --no-progress coreutils aws-cli

# Make sure we have a local copy of the yum repo
echo "--- Syncing s3://$RPM_S3_BUCKET to `hostname`"
mkdir -p $YUM_PATH
aws --region us-east-1 s3 sync --delete --only-show-errors "s3://$RPM_S3_BUCKET" "$YUM_PATH"

# Add the rpms and update meta-data
for ARCH in "x86_64" "i386" "aarch64"; do
  echo "--- Updating yum repository for ${CODENAME}/${ARCH}"

  ARCH_PATH="${YUM_PATH}/buildkite-agent/${CODENAME}/${ARCH}"
  mkdir -p "$ARCH_PATH"
  find "rpm/" -type f -name "*${ARCH}*" | xargs cp -t "$ARCH_PATH"

  # createrepo_c is much faster and more resilient than createrepo
  createrepo_c --no-database --unique-md-filenames --retain-old-md-by-age=180d --update "$ARCH_PATH" || \
    createrepo_c --no-database --unique-md-filenames --retain-old-md-by-age=180d "$ARCH_PATH"
done

# Sync back our changes to S3
echo "--- Syncing local $YUM_PATH changes back to s3://$RPM_S3_BUCKET"
dry_run aws --region us-east-1 s3 sync --delete "$YUM_PATH/" "s3://$RPM_S3_BUCKET" --no-guess-mime-type --exclude "lost+found" --exclude ".repodata"
