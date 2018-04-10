#!/bin/bash

set -o errexit

if [[ ${#} -ne 2 ]]
then
  echo "Usage: ${0} [debian-package] [codename]" >&2
  exit 1
fi

function info {
  echo -e "\033[35m$1\033[0m"
}

PACKAGE=${1}
CODENAME=${2}
COMPONENT="main"

# Some validations
if [ -z "$GPG_SIGNING_KEY" ]; then
  echo "Error: Missing ENV variable GPG_SIGNING_KEY"
  exit 1
fi

if [ -z "$GPG_PASSPHRASE_PASSWORD" ]; then
  echo "Error: Missing ENV variable GPG_PASSPHRASE_PASSWORD"
  exit 1
fi

if [ -z "$GPG_PASSPHRASE_PATH" ]; then
  echo "Error: Missing ENV variable GPG_PASSPHRASE_PATH"
  exit 1
fi

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

info "Uploading $PACKAGE to $DEB_S3_BUCKET ($CODENAME $COMPONENT)"

# Decrpyt the GPG_PASSPHRASE with our GPG_PASSPHRASE_PASSWORD
GPG_PASSPHRASE=`openssl aes-256-cbc -k "$GPG_PASSPHRASE_PASSWORD" -in "$GPG_PASSPHRASE_PATH" -d`

deb_s3_args=(
  --preserve-versions
  --sign "$GPG_SIGNING_KEY"
  --gpg-options "\-\-digest-algo SHA512 \-\-passphrase $GPG_PASSPHRASE"
  --codename "$CODENAME"
  --component "$COMPONENT"
  "--access-key-id=$DEB_S3_ACCESS_KEY_ID"
  "--secret-access-key=$DEB_S3_SECRET_ACCESS_KEY_ID"
)

# Older versions were ok with prefix and bucket in the same parameter, but we now need to split them

echo "Parsing DEB_S3_BUCKET=$DEB_S3_BUCKET"
DEB_S3_BUCKET_ARRAY=(${DEB_S3_BUCKET//\// })

if [[ ${#DEB_S3_BUCKET_ARRAY[@]} -gt 2 ]] ; then
  echo "Expected $DEB_S3_BUCKET to have at most 1 path component"
fi

if [[ ${#DEB_S3_BUCKET_ARRAY[@]} -gt 1 ]] ; then
  deb_s3_args+=(
    --bucket "${DEB_S3_BUCKET_ARRAY[0]}"
    --prefix "${DEB_S3_BUCKET_ARRAY[1]}"
  )
else
  deb_s3_args+=(
    --bucket "$DEB_S3_BUCKET_PREFIX"
  )
fi

# Uploads to s3 and signs with the default key on the system

bundle exec deb-s3 upload "${deb_s3_args[@]}" "$PACKAGE"

echo "✅ All done! To install this package:"
echo ""
echo "    # Login as root"
echo "    $ sudo su"
echo ""
echo "    # Add the repository to your APT sources"
echo "    $ echo deb http://$DEB_S3_BUCKET $CODENAME $COMPONENT > /etc/apt/sources.list.d/buildkite-agent.list"
echo ""
echo "    # Then import the repository key"
echo "    $ apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys 32A37959C2FA5C3C99EFBC32A79206696452D198"
echo ""
echo "    # Install the agent"
echo "    $ apt-get update"
echo "    $ apt-get install -y buildkite-agent"
echo ""
