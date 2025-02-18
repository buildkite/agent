#!/usr/bin/env bash
set -euo pipefail

artifacts_build="$(buildkite-agent meta-data get "agent-artifacts-build")"

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

# createrepo_c is much faster and more resilient than createrepo
createrepo() {
  # Ignores old metadata, builds repodata from scratch.
  createrepo_c \
    --no-database \
    --unique-md-filenames \
    --retain-old-md-by-age=180d \
    "$@"
}

updaterepo() {
  # Reuses the old package metadata, and add new packages with --pkglist.
  # createrepo_c tests that pkglist is a _regular_ file, so we can't use
  # a Bash process substitution i.e. <(find ...)
  # --skip-stat prevents createrepo_c from trying to stat all the RPMs that
  # aren't synced here.
  # Note also that createrepo_c appends pkglist lines to the path it is given
  # to find files.
  pkglist="$(mktemp pkglist.XXXXXXXX)"
  find "$1" -type f -name '*.rpm' | awk -F/ '{print $NF}' > "${pkglist}"
  createrepo_c \
    --no-database \
    --unique-md-filenames \
    --retain-old-md-by-age=180d \
    --skip-stat \
    --update \
    --pkglist "${pkglist}" \
    --recycle-pkglist \
    "$@"
  rm "${pkglist}"
}

sync_from_s3() {
  echo "--- Syncing $1 to $2 on $(hostname)"
  aws --region us-east-1 \
    s3 sync \
    --delete \
    --only-show-errors \
    "$1" "$2"
  du -hs "$2"
}

if [[ "${CODENAME}" == "" ]]; then
  echo "Error: Missing \$CODENAME (stable or unstable)"
  exit 1
fi

YUM_PATH="/yum.buildkite.com"

echo '--- Downloading built yum packages'
rm -rf rpm
mkdir -p rpm
buildkite-agent artifact download --build "${artifacts_build}" "rpm/*.rpm" rpm/

echo '--- Installing dependencies'
apt update
DEBIAN_FRONTEND=noninteractive apt install -y createrepo-c awscli

mkdir -p "${YUM_PATH}"

# Add the RPMs and update meta-data
for ARCH in "x86_64" "i386" "aarch64"; do
  ARCH_PATH="${YUM_PATH}/buildkite-agent/${CODENAME}/${ARCH}"
  mkdir -p "${ARCH_PATH}/repodata"

  # Only sync /repodata - no need for all the old packages (hopefully)
  sync_from_s3 \
    "s3://${RPM_S3_BUCKET}/buildkite-agent/${CODENAME}/${ARCH}/repodata" \
    "${ARCH_PATH}/repodata"

  # Copy the new RPMs in.
  find "rpm/" -type f -name "*${ARCH}*" | xargs cp -t "${ARCH_PATH}"

  echo "--- Updating yum repository for ${CODENAME}/${ARCH}"
  if updaterepo "${ARCH_PATH}"; then
    continue
  fi

  # Quick update failed - fall back to recreating the repo.
  # createrepo_c probably left a temp .repodata lying around.
  rm -fr "${ARCH_PATH}/.repodata"

  # Next we will need all the old RPMs.
  sync_from_s3 \
    "s3://${RPM_S3_BUCKET}/buildkite-agent/${CODENAME}/${ARCH}" \
    "${ARCH_PATH}"

  # Copy the new RPMs in again.
  find "rpm/" -type f -name "*${ARCH}*" | xargs cp -t "${ARCH_PATH}"

  echo "--- Recreating yum repository for ${CODENAME}/${ARCH}"
  createrepo "${ARCH_PATH}"
done

# Sync back our changes to S3
echo "--- Syncing local ${YUM_PATH} changes back to s3://${RPM_S3_BUCKET}"
du -hs "${YUM_PATH}"
dry_run aws --region us-east-1 \
  s3 sync \
  --no-guess-mime-type \
  --exclude "lost+found" \
  --exclude ".repodata" \
  "${YUM_PATH}/" "s3://${RPM_S3_BUCKET}"
