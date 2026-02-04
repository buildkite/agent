#!/usr/bin/env bash

set -o errexit

if [[ ${#} -ne 2 ]]
then
  echo "Usage: ${0} [debian-package] [codename]" >&2
  exit 1
fi

function info {
  echo -e "\033[35m$1\033[0m"
}

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

PACKAGE=${1}
CODENAME=${2}
COMPONENT="main"

# Some validations
if [ -z "$GPG_SIGNING_KEY" ]; then
  echo "Error: Missing ENV variable GPG_SIGNING_KEY"
  exit 1
fi

if [ -z "$DEB_S3_BUCKET" ]; then
  echo "Error: Missing ENV variable DEB_S3_BUCKET"
  exit 1
fi

info "Uploading $PACKAGE to $DEB_S3_BUCKET ($CODENAME $COMPONENT)"

deb_s3_args=(
  --preserve-versions
  --sign "$GPG_SIGNING_KEY"
  --gpg-options "\-\-digest-algo SHA512"
  --codename "$CODENAME"
  --component "$COMPONENT"
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

dry_run bundle exec deb-s3 upload "${deb_s3_args[@]}" "$PACKAGE"

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
