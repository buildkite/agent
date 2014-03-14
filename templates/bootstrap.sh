#!/bin/bash
set -e
set -x

env | grep BUILDBOX

# Create the build directory
BUILD_DIR="tmp/$BUILDBOX_AGENT_NAME/$BUILDBOX_PROJECT_SLUG"
mkdir -p $BUILD_DIR
cd $BUILD_DIR

# Do we need to do a git checkout?
if [ ! -d ".git" ]; then
  git clone "$BUILDBOX_REPO" . -qv
fi

echo '--- preparing git'

git clean -fdq
git fetch -q
git reset --hard origin/master
git checkout -qf "$BUILDBOX_COMMIT"

echo "--- running $BUILDBOX_SCRIPT_PATH"

`."/$BUILDBOX_SCRIPT_PATH"`
EXIT_STATUS=$?

echo "--- uploading artifacts"
buildbox-artifact upload "$BUILDBOX_ARTIFACT_PATHS"

exit $EXIT_STATUS
