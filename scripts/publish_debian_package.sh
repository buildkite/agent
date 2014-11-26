#!/bin/bash

set -o errexit

DEB_CODENAME="buildbox-agent"

if [[ ${#} -ne 1 ]]
then
  echo "Usage: ${0} [debian-package]" >&2
  exit 1
fi
PACKAGE=${1} && shift

# Some validations
if [ -z "$APT_S3_BUCKET" ]; then
  echo "Error: Missing ENV variable APT_S3_BUCKET"
  exit 1
fi

if [ -z "$APT_S3_ACCESS_KEY_ID" ]; then
  echo "Error: Missing ENV variable APT_S3_ACCESS_KEY_ID"
  exit 1
fi

if [ -z "$APT_S3_SECRET_ACCESS_KEY_ID" ]; then
  echo "Error: Missing ENV variable APT_S3_SECRET_ACCESS_KEY_ID"
  exit 1
fi

echo "--- Uploading $PACKAGE to $APT_S3_BUCKET"

deb-s3 upload \
  --bucket $APT_S3_BUCKET \
  --codename $DEB_CODENAME \
  --access-key-id=$APT_S3_ACCESS_KEY_ID \
  --secret-access-key=$APT_S3_SECRET_ACCESS_KEY_ID \
  $PACKAGE
