#!/usr/bin/env bash
set -euo pipefail

artifacts_build=$(buildkite-agent meta-data get "agent-artifacts-build" )

# /root/.gnupg is a tmpfs volume, so we can safely store key data there and know
# it won't be written to a disk
secret_key_path="/root/.gnupg/gpg-secret.gpg"
public_key_path="/root/.gnupg/gpg-public.gpg"

if [[ "$CODENAME" == "" ]]; then
  echo "Error: Missing \$CODENAME (stable or unstable)"
  exit 1
fi

echo '--- Configuring gnupg'

echo "confirming gnupg config is stored in memory, not on disk"

apk add --update findmnt
if ! findmnt --source tmpfs --target /root/.gnupg; then
  echo "/root/.gnupg must be mounted as tmpfs to ensure private keys aren't written to disk"
  exit 1
fi

echo "fetching signing key..."
export GPG_SIGNING_KEY=$(aws ssm get-parameter --name /pipelines/agent/GPG_SIGNING_KEY --with-decryption --output text --query Parameter.Value --region us-east-1)

echo "fetching secret key..."
aws ssm get-parameter --name /pipelines/agent/GPG_SECRET_KEY_ASCII --with-decryption --output text --query Parameter.Value --region us-east-1 > ${secret_key_path}
ls -lh /root/.gnupg/
gpg --import --batch ${secret_key_path}
rm ${secret_key_path}

# technically we don't need the public key for signing, but it's helpful to keep a copy
# in the same place as the secret key so we don't lose it
echo "fetching public key..."
aws ssm get-parameter --name /pipelines/agent/GPG_PUBLIC_KEY_ASCII --with-decryption --output text --query Parameter.Value --region us-east-1 > ${public_key_path}
gpg --import --batch ${public_key_path}
rm ${public_key_path}

echo '--- Downloading built debian packages'
rm -rf deb
mkdir -p deb
buildkite-agent artifact download --build "$artifacts_build" "deb/*.deb" deb/

echo '--- Installing dependencies'
bundle

# Loop over all the .deb files and publish them
for file in deb/*.deb; do
  echo "+++ Publishing $file"
  ./scripts/publish-debian-package.sh "$file" "$CODENAME"
done
