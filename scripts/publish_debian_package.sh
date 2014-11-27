#!/bin/bash

set -o errexit

if [[ ${#} -ne 2 ]]
then
  echo "Usage: ${0} [debian-package] [codename]" >&2
  exit 1
fi

PACKAGE=${1}
CODENAME=${2}
COMPONENT="main"

# Some validations
if [ -z "$DEB_S3_BUCKET" ]; then
  echo "Error: Missing ENV variable DEB_S3_BUCKET"
  exit 1
fi

if [ -z "$DEB_S3_ACCESS_KEY_ID" ]; then
  echo "Error: Missing ENV variable DEB_S3_ACCESS_KEY_ID"
  exit 1
fi

if [ -z "$DEB_S3_SECRET_ACCESS_KEY_ID" ]; then
  echo "Error: Missing ENV variable DEB_S3_SECRET_ACCESS_KEY_ID"
  exit 1
fi

echo "--- Uploading $PACKAGE to $DEB_S3_BUCKET ($CODENAME $COMPONENT)"

deb-s3 upload \
  --bucket $DEB_S3_BUCKET \
  --codename $CODENAME \
  --component $COMPONENT \
  --access-key-id=$DEB_S3_ACCESS_KEY_ID \
  --secret-access-key=$DEB_S3_SECRET_ACCESS_KEY_ID \
  $PACKAGE

echo "✅ All done! To install:"
echo ""
echo "    # Add the repository to your APT sources"
echo "    $ echo deb $DEB_S3_BUCKET $CODENAME $COMPONENT > /etc/apt/sources.list.d/buildbox-agent.list"
echo ""
echo "    # Then import the repository key (TODO)"
echo "    $ apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys xxx"
echo ""
echo "    # Install the agent"
echo "    $ apt-get update"
echo "    $ apt-get install -y buildbox-agent"
