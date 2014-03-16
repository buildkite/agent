#!/bin/bash

BUILDBOX_PROMPT="\033[90m$\033[0m"

function buildbox-exit-if-failed {
  if [ $1 -ne 0 ]
  then
    exit $1
  fi
}

function buildbox-run {
  echo -e "$BUILDBOX_PROMPT $1"
  eval $1
  buildbox-exit-if-failed $?
}

echo '--- setup environment'

# Create the build directory
BUILD_DIR="$BUILDBOX_AGENT_NAME/$BUILDBOX_PROJECT_SLUG"

buildbox-run "mkdir -p $BUILD_DIR"
buildbox-run "cd $BUILD_DIR"

# Do we need to do a git checkout?
if [ ! -d ".git" ]
then
  buildbox-run "git clone "$BUILDBOX_REPO" . -qv"
fi

buildbox-run "git clean -fdq"
buildbox-run "git fetch -q"
buildbox-run "git reset --hard origin/master"
buildbox-run "git checkout -qf \"$BUILDBOX_COMMIT\""

echo "--- running $BUILDBOX_SCRIPT_PATH"

if [ "$BUILDBOX_SCRIPT_PATH" == "" ]
then
  echo "ERROR: No script path has been set for this project. Please go to \"Project Settings\" and add the path to your build script."
  exit 1
else
  ."/$BUILDBOX_SCRIPT_PATH"
  EXIT_STATUS=$?
fi

if [ "$BUILDBOX_ARTIFACT_PATHS" != "" ]
then
  # Test to make sure buildbox-artifact is in the $PATH
  command -v buildbox-artifact >/dev/null 2>&1 || { echo >&2 "ERROR: buildbox-artifact could not be found in \$PATH"; exit 1; }

  # If you want to upload artifacts to your own server, uncomment the lines below
  # and replace the AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY with keys to your
  # own bucket.
  # export AWS_SECRET_ACCESS_KEY=yyy
  # export AWS_ACCESS_KEY_ID=xxx
  # buildbox-artifact upload "$BUILDBOX_ARTIFACT_PATHS" s3://bucket-name/foo/bar --url $BUILDBOX_AGENT_API_URL

  # By default we silence the buildbox-artifact build output. However, if you'd like to see
  # it in your logs, remove the: > /dev/null 2>&1 from the end of the line.
  buildbox-artifact upload "$BUILDBOX_ARTIFACT_PATHS" --url $BUILDBOX_AGENT_API_URL > /dev/null 2>&1
  buildbox-exit-if-failed $?
fi

exit $EXIT_STATUS
