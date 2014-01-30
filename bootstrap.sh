#!/bin/bash
set -e
set -x

cd $BUILDBOX_BUILD_PATH

if [ ! -d ".git" ]; then
  git clone "$BUILDBOX_REPO" . -qv
fi

echo '--- preparing git'

git clean -fdq
git fetch -q
git reset --hard origin/master # always reset back to master
git checkout -qf "$BUILDBOX_COMMIT"
