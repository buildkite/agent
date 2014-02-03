#!/bin/bash
set -e
set -x

env | grep BUILDBOX

mkdir tmp
cd tmp

if [ ! -d ".git" ]; then
  git clone "$BUILDBOX_REPO" . -qv
fi

echo '--- preparing git'

git clean -fdq
git fetch -q
git reset --hard origin/master
git checkout -qf "$BUILDBOX_COMMIT"

echo "--- running $BUILDBOX_SCRIPT_PATH"

."/$BUILDBOX_SCRIPT_PATH"
